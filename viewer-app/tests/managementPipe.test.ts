import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { createServer } from "node:net";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { ManagementConnection } from "../src/managementPipe.ts";

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
