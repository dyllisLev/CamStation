import assert from "node:assert/strict";
import { createServer } from "node:net";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { ManagementConnection } from "../src/managementPipe.ts";

test("management IPC connects without Agent launch identity and acquires a lease", async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), "camstation-viewer-management-"));
  const socketPath = path.join(directory, "service.sock");
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
    await rm(directory, { recursive: true, force: true });
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
