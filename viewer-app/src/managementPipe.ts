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

export type ViewerCommand =
  | { readonly type: "reload_live"; readonly operationKey: string }
  | { readonly type: "resubscribe_stream"; readonly streamName: string; readonly operationKey: string }
  | { readonly type: "restart_viewer"; readonly operationKey: string };

type Response = {
  readonly version: number;
  readonly requestId: string;
  readonly ok: boolean;
  readonly errorCode?: string;
  readonly message?: string;
  readonly payload?: unknown;
};

type EventEnvelope = {
  readonly version: number;
  readonly event: string;
  readonly eventId: string;
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
  #commandHandlers = new Set<(command: ViewerCommand) => void | Promise<void>>();
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
    void this.request("renderer_status", { ...objectPayload(payload), leaseId }).catch(() => undefined);
  }

  reportStream(leaseId: string, payload: unknown): void {
    void this.request("stream_telemetry", { ...objectPayload(payload), leaseId }).catch(() => undefined);
  }

  reportDiagnostic(leaseId: string, payload: unknown): void {
    void this.request("diagnostic_event", { ...objectPayload(payload), leaseId }).catch(() => undefined);
  }

  reportCommandResult(leaseId: string, operationKey: string, succeeded: boolean, errorCode?: string): Promise<void> {
    const payload = succeeded
      ? { leaseId, operationKey, succeeded: true }
      : { leaseId, operationKey, succeeded: false, errorCode: errorCode || "viewer_command_failed" };
    return this.request("command_result", payload).then(() => undefined);
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

  onCommand(handler: (command: ViewerCommand) => void | Promise<void>): () => void {
    this.#commandHandlers.add(handler);
    return () => this.#commandHandlers.delete(handler);
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
      let decoded: unknown;
      try {
        decoded = JSON.parse(line.toString("utf8")) as unknown;
      } catch {
        this.socket.destroy(new Error("invalid management response"));
        return;
      }
      if (isEventEnvelope(decoded)) {
        const command = viewerCommandFromEvent(decoded);
        if (!command) {
          this.socket.destroy(new Error("invalid management event"));
          return;
        }
        for (const handler of this.#commandHandlers) void handler(command);
        continue;
      }
      if (!isResponse(decoded)) {
        this.socket.destroy(new Error("invalid management response"));
        return;
      }
      const response = decoded;
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

function isResponse(value: unknown): value is Response {
  if (!value || typeof value !== "object") return false;
  const response = value as Record<string, unknown>;
  return response.version === MANAGEMENT_PROTOCOL_VERSION && typeof response.requestId === "string"
    && response.requestId.length > 0 && typeof response.ok === "boolean";
}

function isEventEnvelope(value: unknown): value is EventEnvelope {
  if (!value || typeof value !== "object") return false;
  const event = value as Record<string, unknown>;
  return event.version === MANAGEMENT_PROTOCOL_VERSION && typeof event.event === "string"
    && typeof event.eventId === "string" && event.eventId.length > 0 && !("requestId" in event);
}

export function viewerCommandFromEvent(event: EventEnvelope): ViewerCommand | null {
  if (event.event !== "viewer_command" || !event.payload || typeof event.payload !== "object") return null;
  const payload = event.payload as Record<string, unknown>;
  if (!safeOperationKey(payload.operationKey)) return null;
  if (payload.type === "reload_live" || payload.type === "restart_viewer") {
    return { type: payload.type, operationKey: payload.operationKey };
  }
  if (payload.type === "resubscribe_stream" && safeStreamName(payload.streamName)) {
    return { type: "resubscribe_stream", streamName: payload.streamName, operationKey: payload.operationKey };
  }
  return null;
}

function safeOperationKey(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= 128 && /^[a-z0-9._-]+$/iu.test(value);
}

function safeStreamName(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0 || value.length > 128 || value !== value.trim()) return false;
  if (/^[a-z][a-z0-9+.-]*:/iu.test(value) || value.startsWith("//")) return false;
  return !Array.from(value).some((character) => {
    const code = character.charCodeAt(0);
    return code <= 31 || (code >= 127 && code <= 159);
  });
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
