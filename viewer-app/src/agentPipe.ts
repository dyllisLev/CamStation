import net from "node:net";
import { randomUUID } from "node:crypto";

export const PIPE_PROTOCOL_VERSION = 1;
export const MAX_PIPE_MESSAGE_BYTES = 64 * 1024;
export const VIEWER_PIPE_NAME = String.raw`\\.\pipe\CamStationViewerAgent`;
export const LOCAL_HEARTBEAT_MS = 5_000;

export type PipeMessage = {
  readonly version: number;
  readonly requestId: string;
  readonly type: string;
  readonly pid?: number;
  readonly sessionId?: number;
  readonly generation?: number;
  readonly nonce?: string;
  readonly payload?: unknown;
};

export type LaunchIdentity = { readonly generation: number; readonly nonce: string; readonly sessionId: number };
export type ViewerCommand =
  | { readonly type: "reload_live"; readonly operationKey?: string }
  | { readonly type: "resubscribe_stream"; readonly streamName: string; readonly operationKey?: string }
  | { readonly type: "shutdown"; readonly operationKey?: string };

type PendingRequest = { readonly resolve: (message: PipeMessage) => void; readonly reject: (error: Error) => void };

export function encodePipeMessage(message: PipeMessage): Buffer {
  if (message.version !== PIPE_PROTOCOL_VERSION || !message.requestId || !message.type) {
    throw new Error("invalid Agent pipe message");
  }
  const encoded = Buffer.from(`${JSON.stringify(message)}\n`, "utf8");
  if (encoded.length > MAX_PIPE_MESSAGE_BYTES) throw new Error("Agent pipe message exceeds 64 KiB");
  return encoded;
}

export class PipeDecoder {
  #buffer = Buffer.alloc(0);

  push(chunk: Buffer): PipeMessage[] {
    this.#buffer = Buffer.concat([this.#buffer, chunk]);
    const messages: PipeMessage[] = [];
    for (;;) {
      const newline = this.#buffer.indexOf(10);
      if (newline < 0) {
        if (this.#buffer.length >= MAX_PIPE_MESSAGE_BYTES) throw new Error("Agent pipe message exceeds 64 KiB");
        return messages;
      }
      if (newline + 1 > MAX_PIPE_MESSAGE_BYTES) throw new Error("Agent pipe message exceeds 64 KiB");
      const line = this.#buffer.subarray(0, newline);
      this.#buffer = this.#buffer.subarray(newline + 1);
      if (line.length === 0) throw new Error("empty Agent pipe message");
      const parsed: unknown = JSON.parse(line.toString("utf8"));
      if (!isPipeMessage(parsed)) throw new Error("invalid Agent pipe message");
      messages.push(parsed);
    }
  }
}

export function parseLaunchIdentity(args: readonly string[]): LaunchIdentity {
  const generation = integerArgument(args, "--agent-generation=");
  const sessionId = integerArgument(args, "--agent-session=", true);
  const nonce = stringArgument(args, "--agent-nonce=");
  if (generation <= 0 || nonce.length > 256 || !/^[\x21-\x7e]+$/u.test(nonce)) throw new Error("invalid Agent launch identity");
  return { generation, nonce, sessionId };
}

export function isViewerCommand(input: unknown): input is ViewerCommand {
  if (!input || typeof input !== "object") return false;
  const command = input as Record<string, unknown>;
  if (typeof command.operationKey !== "string" || command.operationKey.length === 0 || command.operationKey.length > 256) return false;
  if (command.type === "reload_live" || command.type === "shutdown") return true;
  return command.type === "resubscribe_stream" && safeStreamName(command.streamName);
}

export class AgentConnection {
  readonly serverURL: string;
  readonly launchNonce: string;
  readonly generation: number;
  #socket: net.Socket;
  #identity: LaunchIdentity;
  #pending = new Map<string, PendingRequest>();
  #commandHandlers = new Set<(command: ViewerCommand) => void | Promise<void>>();
  #shutdownHandlers = new Set<() => void>();
  #disconnectHandlers = new Set<(error: Error) => void>();
  #heartbeat: NodeJS.Timeout;
  #requestTimeoutMs: number;
  #closed = false;
  #disconnectError: Error | null = null;

  private constructor(socket: net.Socket, identity: LaunchIdentity, serverURL: string, launchNonce: string, requestTimeoutMs: number) {
    this.#socket = socket;
    this.#identity = identity;
    this.serverURL = serverURL;
    this.launchNonce = launchNonce;
    this.generation = identity.generation;
    this.#requestTimeoutMs = requestTimeoutMs;
    this.#heartbeat = setInterval(() => void this.request("viewer_heartbeat").catch(() => undefined), LOCAL_HEARTBEAT_MS);
    this.#heartbeat.unref();
  }

  static async connect(identity: LaunchIdentity, pipeName = VIEWER_PIPE_NAME, requestTimeoutMs = 5_000): Promise<AgentConnection> {
    const socket = await connectSocket(pipeName);
    const decoder = new PipeDecoder();
    const pending = new Map<string, PendingRequest>();
    const fail = (error: Error) => {
      for (const request of pending.values()) request.reject(error);
      pending.clear();
    };
    socket.on("error", fail);
    socket.on("close", () => fail(new Error("Agent pipe closed")));
    socket.on("data", (chunk: Buffer) => {
      try {
        for (const message of decoder.push(chunk)) {
          const request = pending.get(message.requestId);
          if (!request) continue;
          pending.delete(message.requestId);
          request.resolve(message);
        }
      } catch (error) {
        socket.destroy(error instanceof Error ? error : new Error("invalid Agent pipe response"));
      }
    });
    const requestId = randomUUID();
    let response: PipeMessage;
    try {
      response = await sendPipeRequest(socket, pending, {
        version: PIPE_PROTOCOL_VERSION,
        requestId,
        type: "viewer_register",
        pid: process.pid,
        sessionId: identity.sessionId,
        generation: identity.generation,
        nonce: identity.nonce,
      }, requestTimeoutMs, "Agent registration timed out");
    } catch (error) {
      socket.destroy();
      throw error;
    }
    if (response.type !== "viewer_registered" || response.generation !== identity.generation || response.nonce !== identity.nonce) {
      socket.destroy();
      throw new Error("Agent rejected Viewer registration");
    }
    const serverURL = serverURLFromPayload(response.payload);
    const connection = new AgentConnection(socket, identity, serverURL, response.nonce, requestTimeoutMs);
    connection.#pending = pending;
    socket.removeAllListeners("data");
    socket.on("data", (chunk: Buffer) => connection.#receive(decoder, chunk));
    socket.removeAllListeners("error");
    socket.removeAllListeners("close");
    socket.on("error", (error) => connection.#fail(error));
    socket.on("close", () => connection.#fail(new Error("Agent pipe closed")));
    return connection;
  }

  reportRenderer(payload: unknown): void {
    void this.request("renderer_status", payload).catch(() => undefined);
  }

  reportStream(payload: unknown): void {
    void this.request("stream_telemetry", payload).catch(() => undefined);
  }

  onCommand(handler: (command: ViewerCommand) => void | Promise<void>): () => void {
    this.#commandHandlers.add(handler);
    return () => this.#commandHandlers.delete(handler);
  }

  onShutdown(handler: () => void): () => void {
    this.#shutdownHandlers.add(handler);
    return () => this.#shutdownHandlers.delete(handler);
  }

  onDisconnect(handler: (error: Error) => void): () => void {
    if (this.#disconnectError) {
      handler(this.#disconnectError);
      return () => undefined;
    }
    this.#disconnectHandlers.add(handler);
    return () => this.#disconnectHandlers.delete(handler);
  }

  pendingRequestCount(): number {
    return this.#pending.size;
  }

  close(): void {
    if (this.#closed) return;
    this.#shutdown(new Error("Agent pipe closed"), false);
    this.#socket.end();
  }

  private async request(type: string, payload?: unknown): Promise<PipeMessage> {
    if (this.#closed) throw new Error("Agent pipe is closed");
    const requestId = randomUUID();
    return sendPipeRequest(this.#socket, this.#pending, {
      version: PIPE_PROTOCOL_VERSION,
      requestId,
      type,
      pid: process.pid,
      sessionId: this.#identity.sessionId,
      generation: this.#identity.generation,
      nonce: this.#identity.nonce,
      payload,
    }, this.#requestTimeoutMs, "Agent pipe response timed out");
  }

  #receive(decoder: PipeDecoder, chunk: Buffer): void {
    try {
      for (const message of decoder.push(chunk)) {
        const request = this.#pending.get(message.requestId);
        if (request) {
          this.#pending.delete(message.requestId);
          request.resolve(message);
        }
        const command = message.payload;
        if (message.type === "command" && isViewerCommand(command)) {
          void executeViewerCommand(command, [...this.#commandHandlers])
            .then((payload) => this.request("command_result", payload))
            .then(() => {
              if (command.type === "shutdown") {
                for (const handler of this.#shutdownHandlers) handler();
              }
            })
            .catch(() => undefined);
        }
      }
    } catch (error) {
      this.#socket.destroy(error instanceof Error ? error : new Error("invalid Agent pipe response"));
    }
  }

  #fail(error: Error): void {
    this.#shutdown(error, true);
  }

  #shutdown(error: Error, unexpected: boolean): void {
    if (this.#closed) return;
    this.#closed = true;
    clearInterval(this.#heartbeat);
    for (const request of this.#pending.values()) request.reject(error);
    this.#pending.clear();
    if (unexpected) {
      this.#disconnectError = error;
      for (const handler of this.#disconnectHandlers) handler(error);
    }
  }
}

export async function executeViewerCommand(
  command: ViewerCommand,
  handlers: readonly ((command: ViewerCommand) => void | Promise<void>)[],
): Promise<{ readonly operationKey: string; readonly succeeded: boolean }> {
  const operationKey = command.operationKey;
  if (typeof operationKey !== "string" || operationKey.length === 0 || operationKey.length > 256) {
    throw new Error("Viewer command operation key is required");
  }
  try {
    if (handlers.length === 0) throw new Error("Viewer command handler is unavailable");
    await Promise.all(handlers.map((handler) => handler(command)));
    return { operationKey, succeeded: true };
  } catch {
    return { operationKey, succeeded: false };
  }
}

function isPipeMessage(input: unknown): input is PipeMessage {
  if (!input || typeof input !== "object") return false;
  const message = input as Record<string, unknown>;
  return message.version === PIPE_PROTOCOL_VERSION && typeof message.requestId === "string" && message.requestId.length > 0
    && typeof message.type === "string" && message.type.length > 0;
}

function integerArgument(args: readonly string[], prefix: string, zeroAllowed = false): number {
  const values = args.filter((arg) => arg.startsWith(prefix));
  if (values.length !== 1) throw new Error(`missing ${prefix}`);
  const value = Number(values[0].slice(prefix.length));
  if (!Number.isSafeInteger(value) || value < (zeroAllowed ? 0 : 1)) throw new Error(`invalid ${prefix}`);
  return value;
}

function stringArgument(args: readonly string[], prefix: string): string {
  const values = args.filter((arg) => arg.startsWith(prefix));
  if (values.length !== 1 || values[0].length === prefix.length) throw new Error(`missing ${prefix}`);
  return values[0].slice(prefix.length);
}

function safeStreamName(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0 || value.length > 128 || value !== value.trim()) return false;
  if (/^[a-z][a-z0-9+.-]*:/iu.test(value) || value.startsWith("//")) return false;
  return !Array.from(value).some((character) => {
    const code = character.charCodeAt(0);
    return code <= 31 || (code >= 127 && code <= 159);
  });
}

function serverURLFromPayload(payload: unknown): string {
  if (!payload || typeof payload !== "object") throw new Error("Agent response omitted server URL");
  const serverURL = (payload as Record<string, unknown>).serverUrl;
  if (typeof serverURL !== "string" || serverURL.length > 2_048) throw new Error("Agent response omitted server URL");
  return serverURL;
}

function connectSocket(pipeName: string): Promise<net.Socket> {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(pipeName);
    socket.once("connect", () => resolve(socket));
    socket.once("error", reject);
  });
}

function sendPipeRequest(
  socket: net.Socket,
  pending: Map<string, PendingRequest>,
  message: PipeMessage,
  timeoutMs: number,
  timeoutMessage: string,
): Promise<PipeMessage> {
  const encoded = encodePipeMessage(message);
  return new Promise((resolve, reject) => {
    let settled = false;
    let timer: NodeJS.Timeout;
    const finish = (value?: PipeMessage, error?: Error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      pending.delete(message.requestId);
      if (error) reject(error);
      else resolve(value as PipeMessage);
    };
    timer = setTimeout(() => finish(undefined, new Error(timeoutMessage)), timeoutMs);
    pending.set(message.requestId, {
      resolve: (value) => finish(value),
      reject: (error) => finish(undefined, error),
    });
    try {
      socket.write(encoded, (error?: Error | null) => {
        if (error) finish(undefined, error);
      });
    } catch (error) {
      finish(undefined, error instanceof Error ? error : new Error("Agent pipe write failed"));
    }
  });
}
