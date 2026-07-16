import assert from "node:assert/strict";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { randomUUID } from "node:crypto";
import {
  AgentConnection,
  PipeDecoder,
  encodePipeMessage,
  type PipeMessage,
} from "../src/agentPipe.ts";
import { disconnectExitCode } from "../src/viewerLifecycle.ts";

const identity = { generation: 7, nonce: "nonce-7", sessionId: 3 };

test("unexpected Agent pipe close notifies exactly once while explicit close stays quiet", async () => {
  const peer = await startAgentPeer(true);
  const connection = await AgentConnection.connect(identity, peer.path, 50);
  let disconnects = 0;
  const disconnected = new Promise<Error>((resolve) => {
    connection.onDisconnect((error) => {
      disconnects++;
      resolve(error);
    });
  });
  peer.socket?.destroy();
  await disconnected;
  await delay(5);
  assert.equal(disconnects, 1);
  assert.equal(disconnectExitCode(false), 1);
  connection.close();
  assert.equal(disconnects, 1);
  await peer.close();

  const explicitPeer = await startAgentPeer(true);
  const explicit = await AgentConnection.connect(identity, explicitPeer.path, 50);
  let explicitDisconnects = 0;
  explicit.onDisconnect(() => explicitDisconnects++);
  explicit.close();
  await delay(5);
  assert.equal(explicitDisconnects, 0);
  assert.equal(disconnectExitCode(true), null);
  await explicitPeer.close();
});

test("an Agent disconnect before lifecycle subscription is retained", async () => {
  const peer = await startAgentPeer(true);
  const connection = await AgentConnection.connect(identity, peer.path, 50);
  peer.socket?.destroy();
  await delay(5);
  let disconnects = 0;
  connection.onDisconnect(() => disconnects++);
  assert.equal(disconnects, 1);
  connection.close();
  await peer.close();
});

test("registration and repeated request timeouts leave no pending entries", async () => {
  const silentRegistration = await startAgentPeer(false);
  await assert.rejects(() => AgentConnection.connect(identity, silentRegistration.path, 10));
  await silentRegistration.close();

  const peer = await startAgentPeer(true);
  const connection = await AgentConnection.connect(identity, peer.path, 10);
  for (let attempt = 0; attempt < 3; attempt++) {
    connection.reportRenderer({ state: "ready" });
    await delay(20);
    assert.equal(connection.pendingRequestCount(), 0);
  }
  connection.reportStream({ value: "x".repeat(70 * 1024) });
  await delay(1);
  assert.equal(connection.pendingRequestCount(), 0);
  connection.close();
  await peer.close();
});

async function startAgentPeer(register: boolean): Promise<{
  path: string;
  socket: net.Socket | null;
  close(): Promise<void>;
}> {
  const socketPath = path.join(os.tmpdir(), `camstation-viewer-${randomUUID()}.sock`);
  let socket: net.Socket | null = null;
  const server = net.createServer((connection) => {
    socket = connection;
    if (!register) return;
    const decoder = new PipeDecoder();
    connection.on("data", (chunk) => {
      for (const message of decoder.push(chunk)) {
        if (message.type !== "viewer_register") continue;
        const response: PipeMessage = {
          version: 1,
          requestId: message.requestId,
          type: "viewer_registered",
          generation: identity.generation,
          nonce: identity.nonce,
          payload: { serverUrl: "http://10.0.0.5:18080" },
        };
        connection.write(encodePipeMessage(response));
      }
    });
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(socketPath, resolve);
  });
  return {
    path: socketPath,
    get socket() {
      return socket;
    },
    close: async () => {
      socket?.destroy();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    },
  };
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
