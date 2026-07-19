import { randomUUID } from "node:crypto";
import net from "node:net";

export const MANAGEMENT_PIPE_NAME = String.raw`\\.\pipe\CamStationViewerService`;
export const MANAGEMENT_PROTOCOL_VERSION = 2;
export const MAX_MANAGEMENT_MESSAGE_BYTES = 64 * 1024;

export type ConfigDraft = {
  readonly serverUrl: string;
  readonly displayName: string;
  readonly autoStart: boolean;
};

export type ViewerStatus = {
  readonly configured: boolean;
  readonly config?: { readonly serverUrl: string; readonly displayName: string };
  readonly connection: "unconfigured" | "connecting" | "online" | "offline" | "service_unavailable";
  readonly autoStart: boolean;
  readonly leaseAvailable: boolean;
};

export type LeaseGrant = {
  readonly leaseId: string;
  readonly heartbeatSeconds: number;
  readonly logPath?: string;
};

type Response = {
  readonly version: number;
  readonly requestId: string;
  readonly ok: boolean;
  readonly errorCode?: string;
  readonly message?: string;
  readonly payload?: unknown;
};

type PendingRequest = {
  readonly resolve: (value: unknown) => void;
  readonly reject: (error: Error) => void;
};

export class ManagementRequestError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

export class ManagementConnection {
  #closed = false;
  #buffer = Buffer.alloc(0);
  #pending = new Map<string, PendingRequest>();
  #disconnectHandlers = new Set<(error: Error) => void>();
  private readonly socket: net.Socket;

  private constructor(socket: net.Socket) {
    this.socket = socket;
    socket.on("data", (chunk: Buffer) => this.receive(chunk));
    socket.on("error", (error) => this.fail(error));
    socket.on("close", () => this.fail(new Error("management pipe closed")));
  }

  static async connect(pipeName = MANAGEMENT_PIPE_NAME): Promise<ManagementConnection> {
    const socket = await new Promise<net.Socket>((resolve, reject) => {
      const candidate = net.createConnection(pipeName);
      candidate.once("connect", () => resolve(candidate));
      candidate.once("error", reject);
    });
    return new ManagementConnection(socket);
  }

  async status(): Promise<ViewerStatus> {
    const payload = await this.request("get_status");
    if (!isViewerStatus(payload)) throw new Error("management service returned an invalid status");
    return payload;
  }

  async configure(draft: ConfigDraft): Promise<ViewerStatus> {
    const payload = await this.request("configure", draft);
    if (!isViewerStatus(payload)) throw new Error("management service returned an invalid status");
    return payload;
  }

  async acquireLease(): Promise<LeaseGrant> {
    const payload = await this.request("acquire_lease");
    if (!isLeaseGrant(payload)) throw new Error("management service returned an invalid lease");
    return payload;
  }

  heartbeat(leaseId: string): void {
    void this.request("lease_heartbeat", { leaseId }).catch(() => undefined);
  }

  release(leaseId: string): void {
    void this.request("release_lease", { leaseId }).catch(() => undefined);
  }

  reportViewer(leaseId: string, state: string): void {
    void this.request("viewer_status", { leaseId, state }).catch(() => undefined);
  }

  reportRenderer(leaseId: string, payload: unknown): void {
    void this.request("renderer_status", { leaseId, ...objectPayload(payload) }).catch(() => undefined);
  }

  reportStream(leaseId: string, payload: unknown): void {
    void this.request("stream_telemetry", { leaseId, ...objectPayload(payload) }).catch(() => undefined);
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    this.socket.end();
    this.fail(new Error("management pipe closed"));
  }

  onDisconnect(handler: (error: Error) => void): () => void {
    this.#disconnectHandlers.add(handler);
    return () => this.#disconnectHandlers.delete(handler);
  }

  private request(type: string, payload?: unknown): Promise<unknown> {
    if (this.#closed) return Promise.reject(new Error("management pipe closed"));
    const requestId = randomUUID();
    const request = payload === undefined
      ? { version: MANAGEMENT_PROTOCOL_VERSION, requestId, type }
      : { version: MANAGEMENT_PROTOCOL_VERSION, requestId, type, payload };
    const encoded = Buffer.from(`${JSON.stringify(request)}\n`, "utf8");
    if (encoded.length > MAX_MANAGEMENT_MESSAGE_BYTES) return Promise.reject(new Error("management message exceeds 64 KiB"));
    return new Promise<unknown>((resolve, reject) => {
      this.#pending.set(requestId, { resolve, reject });
      this.socket.write(encoded, (error?: Error | null) => {
        if (error) {
          this.#pending.delete(requestId);
          reject(error);
        }
      });
    });
  }

  private receive(chunk: Buffer): void {
    this.#buffer = Buffer.concat([this.#buffer, chunk]);
    while (true) {
      const newline = this.#buffer.indexOf(10);
      if (newline < 0) {
        if (this.#buffer.length > MAX_MANAGEMENT_MESSAGE_BYTES) this.socket.destroy(new Error("management message exceeds 64 KiB"));
        return;
      }
      const line = this.#buffer.subarray(0, newline);
      this.#buffer = this.#buffer.subarray(newline + 1);
      if (line.length === 0 || line.length > MAX_MANAGEMENT_MESSAGE_BYTES) {
        this.socket.destroy(new Error("invalid management response"));
        return;
      }
      let response: Response;
      try {
        response = JSON.parse(line.toString("utf8")) as Response;
      } catch {
        this.socket.destroy(new Error("invalid management response"));
        return;
      }
      if (response.version !== MANAGEMENT_PROTOCOL_VERSION || typeof response.requestId !== "string" || typeof response.ok !== "boolean") {
        this.socket.destroy(new Error("invalid management response"));
        return;
      }
      const pending = this.#pending.get(response.requestId);
      if (!pending) continue;
      this.#pending.delete(response.requestId);
      if (response.ok) pending.resolve(response.payload);
      else pending.reject(new ManagementRequestError(response.errorCode || "request_failed", response.message || "management request failed"));
    }
  }

  private fail(error: Error): void {
    if (this.#closed && this.#pending.size === 0) return;
    this.#closed = true;
    for (const pending of this.#pending.values()) pending.reject(error);
    this.#pending.clear();
    for (const handler of this.#disconnectHandlers) handler(error);
  }
}

function objectPayload(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function isViewerStatus(value: unknown): value is ViewerStatus {
  if (!value || typeof value !== "object") return false;
  const status = value as Record<string, unknown>;
  return typeof status.configured === "boolean" && typeof status.connection === "string" && typeof status.autoStart === "boolean" && typeof status.leaseAvailable === "boolean";
}

function isLeaseGrant(value: unknown): value is LeaseGrant {
  if (!value || typeof value !== "object") return false;
  const lease = value as Record<string, unknown>;
  return typeof lease.leaseId === "string" && lease.leaseId.length > 0 && Number.isInteger(lease.heartbeatSeconds) && (lease.heartbeatSeconds as number) > 0;
}
