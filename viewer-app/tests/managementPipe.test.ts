import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { createServer } from "node:net";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { ManagementConnection, ManagementRequestError, managementFailureCode } from "../src/managementPipe.ts";

test("management IPC connects without Agent launch identity and acquires a lease", async (t) => {
  const directory = process.platform === "win32"
    ? null
    : await mkdtemp(path.join(tmpdir(), "camstation-viewer-management-"));
  const socketPath = directory === null
    ? String.raw`\\.\pipe\camstation-viewer-management-${process.pid}-${randomUUID()}`
    : path.join(directory, "service.sock");
  const server = createServer((socket) => {
    socket.on("data", (chunk) => {
      const request = JSON.parse(chunk.toString("utf8")) as { requestId: string; type: string };
      const payload = request.type === "get_status"
        ? { configured: true, config: { serverUrl: "http://127.0.0.1:18080", displayName: "벽면" }, connection: "online", autoStart: true, leaseAvailable: true }
        : { leaseId: "lease-1", heartbeatSeconds: 5 };
      socket.write(`${JSON.stringify({ version: 2, requestId: request.requestId, ok: true, payload })}\n`);
    });
  });
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(socketPath, resolve));
  t.after(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    if (directory !== null) await rm(directory, { recursive: true, force: true });
  });

  const connection = await ManagementConnection.connect(socketPath);
  assert.deepEqual(await connection.status(), {
    configured: true,
    config: { serverUrl: "http://127.0.0.1:18080", displayName: "벽면" },
    connection: "online",
    autoStart: true,
    leaseAvailable: true,
  });
  assert.deepEqual(await connection.acquireLease(), { leaseId: "lease-1", heartbeatSeconds: 5 });
  connection.close();
});

test("management IPC separates unsolicited Viewer commands from request responses", async (t) => {
  const directory = process.platform === "win32"
    ? null
    : await mkdtemp(path.join(tmpdir(), "camstation-viewer-events-"));
  const socketPath = directory === null
    ? String.raw`\\.\pipe\camstation-viewer-events-${process.pid}-${randomUUID()}`
    : path.join(directory, "service.sock");
  const server = createServer((socket) => {
    socket.on("data", (chunk) => {
      const request = JSON.parse(chunk.toString("utf8")) as { requestId: string; type: string };
      socket.write(`${JSON.stringify({
        version: 2,
        requestId: request.requestId,
        ok: true,
        payload: { configured: false, connection: "unconfigured", autoStart: true, leaseAvailable: true },
      })}\n${JSON.stringify({
        version: 2,
        event: "viewer_command",
        eventId: "command-42",
        payload: { type: "resubscribe_stream", streamName: "gate-main", operationKey: "command-42" },
      })}\n`);
    });
  });
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(socketPath, resolve));
  t.after(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    if (directory !== null) await rm(directory, { recursive: true, force: true });
  });

  const connection = await ManagementConnection.connect(socketPath);
  const received = new Promise<unknown>((resolve) => connection.onCommand(resolve));
  await connection.status();
  assert.deepEqual(await received, {
    type: "resubscribe_stream",
    streamName: "gate-main",
    operationKey: "command-42",
  });
  connection.close();
});

test("management IPC binds structured diagnostics to the active lease", async (t) => {
  const directory = process.platform === "win32"
    ? null
    : await mkdtemp(path.join(tmpdir(), "camstation-viewer-diagnostic-"));
  const socketPath = directory === null
    ? String.raw`\\.\pipe\camstation-viewer-diagnostic-${process.pid}-${randomUUID()}`
    : path.join(directory, "service.sock");
  let resolveDiagnostic: (value: Record<string, unknown>) => void = () => undefined;
  const diagnostic = new Promise<Record<string, unknown>>((resolve) => {
    resolveDiagnostic = resolve;
  });
  const server = createServer((socket) => {
    socket.on("data", (chunk) => {
      for (const line of chunk.toString("utf8").trim().split("\n")) {
        const request = JSON.parse(line) as { requestId: string; type: string; payload?: Record<string, unknown> };
        if (request.type === "diagnostic_event" && request.payload) resolveDiagnostic(request.payload);
        socket.write(`${JSON.stringify({ version: 2, requestId: request.requestId, ok: true })}\n`);
      }
    });
  });
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(socketPath, resolve));
  t.after(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    if (directory !== null) await rm(directory, { recursive: true, force: true });
  });

  const connection = await ManagementConnection.connect(socketPath);
  connection.reportDiagnostic("lease-123", {
    leaseId: "untrusted-renderer-lease",
    level: "debug",
    component: "viewer.playback",
    event: "first_media",
    sessionId: "playback-12345678",
    streamName: "yard-live",
  });
  assert.deepEqual(await diagnostic, {
    leaseId: "lease-123",
    level: "debug",
    component: "viewer.playback",
    event: "first_media",
    sessionId: "playback-12345678",
    streamName: "yard-live",
  });
  connection.close();
});

test("a silent management request times out, rejects all pending work, and disconnects once", async (t) => {
  const directory = process.platform === "win32"
    ? null
    : await mkdtemp(path.join(tmpdir(), "camstation-viewer-timeout-"));
  const socketPath = directory === null
    ? String.raw`\\.\pipe\camstation-viewer-timeout-${process.pid}-${randomUUID()}`
    : path.join(directory, "service.sock");
  const server = createServer((socket) => socket.on("data", () => undefined));
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(socketPath, resolve));
  t.after(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    if (directory !== null) await rm(directory, { recursive: true, force: true });
  });

  const connection = await ManagementConnection.connect(socketPath, 40);
  let disconnects = 0;
  connection.onDisconnect(() => disconnects++);
  const first = connection.status();
  const second = connection.status();

  await assert.rejects(first, /timed out/iu);
  await assert.rejects(second);
  await assert.rejects(connection.status(), /closed/iu);
  assert.equal(disconnects, 1);
  connection.close();
  assert.equal(disconnects, 1);
});

test("an application-level heartbeat rejection terminates the lease generation", async (t) => {
  const directory = process.platform === "win32"
    ? null
    : await mkdtemp(path.join(tmpdir(), "camstation-viewer-lease-failed-"));
  const socketPath = directory === null
    ? String.raw`\\.\pipe\camstation-viewer-lease-failed-${process.pid}-${randomUUID()}`
    : path.join(directory, "service.sock");
  const server = createServer((socket) => {
    socket.on("data", (chunk) => {
      const request = JSON.parse(chunk.toString("utf8")) as { requestId: string };
      socket.write(`${JSON.stringify({
        version: 2,
        requestId: request.requestId,
        ok: false,
        errorCode: "lease_failed",
        message: "lease failed",
      })}\n`);
    });
  });
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(socketPath, resolve));
  t.after(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()));
    if (directory !== null) await rm(directory, { recursive: true, force: true });
  });

  const connection = await ManagementConnection.connect(socketPath, 100);
  let disconnects = 0;
  connection.onDisconnect(() => disconnects++);

  await assert.rejects(connection.heartbeat("expired-lease"), (error: unknown) => (
    error instanceof Error && "code" in error && error.code === "lease_failed"
  ));
  assert.equal(disconnects, 1);
  await assert.rejects(connection.status(), /closed/iu);
});

test("management recovery diagnostics use only bounded failure classes", () => {
  assert.equal(managementFailureCode(new ManagementRequestError("request_timeout", "raw detail")), "request_timeout");
  assert.equal(managementFailureCode(new ManagementRequestError("connect_timeout", "raw detail")), "connect_timeout");
  assert.equal(managementFailureCode(new ManagementRequestError("lease_failed", "raw detail")), "lease_failed");
  assert.equal(managementFailureCode(new Error("management pipe closed")), "pipe_closed");
  assert.equal(managementFailureCode(Object.assign(new Error("private operating-system detail"), { code: "EPIPE" })), "transport_failed");
});
