import assert from "node:assert/strict";
import test from "node:test";
import {
  MAX_PIPE_MESSAGE_BYTES,
  PipeDecoder,
  encodePipeMessage,
  executeViewerCommand,
  isViewerCommand,
  parseLaunchIdentity,
} from "../src/agentPipe.ts";

test("pipe framing is versioned, newline-delimited, and capped at 64 KiB", () => {
  const message = { version: 1, requestId: "r-1", type: "viewer_heartbeat", pid: 42 };
  assert.equal(encodePipeMessage(message).toString("utf8"), `${JSON.stringify(message)}\n`);
  assert.throws(() => encodePipeMessage({ ...message, payload: { value: "x".repeat(MAX_PIPE_MESSAGE_BYTES) } }));

  const decoder = new PipeDecoder();
  assert.deepEqual(decoder.push(Buffer.from(`${JSON.stringify(message)}\n`)), [message]);
  assert.throws(() => decoder.push(Buffer.alloc(MAX_PIPE_MESSAGE_BYTES + 1, 120)));
});

test("Viewer command results are reported only after the handler completes", async () => {
  let completed = false;
  const result = await executeViewerCommand(
    { type: "reload_live", operationKey: "command-7" },
    [async () => {
      await Promise.resolve();
      completed = true;
    }],
  );
  assert.equal(completed, true);
  assert.deepEqual(result, { operationKey: "command-7", succeeded: true });

  const failed = await executeViewerCommand(
    { type: "shutdown", operationKey: "command-8" },
    [() => {
      throw new Error("renderer failure");
    }],
  );
  assert.deepEqual(failed, { operationKey: "command-8", succeeded: false });
});

test("launch identity accepts only bounded generation, nonce, and session arguments", () => {
  assert.deepEqual(
    parseLaunchIdentity(["--agent-generation=7", "--agent-nonce=abc123", "--agent-session=3"]),
    { generation: 7, nonce: "abc123", sessionId: 3 },
  );
  assert.throws(() => parseLaunchIdentity(["--agent-generation=0", "--agent-nonce=abc123", "--agent-session=3"]));
  assert.throws(() => parseLaunchIdentity(["--agent-generation=7", `--agent-nonce=${"x".repeat(257)}`, "--agent-session=3"]));
});

test("only renderer-safe local commands are accepted", () => {
  assert.equal(isViewerCommand({ type: "reload_live", operationKey: "command-1" }), true);
  assert.equal(isViewerCommand({ type: "resubscribe_stream", streamName: "yard-live", operationKey: "command-2" }), true);
  assert.equal(isViewerCommand({ type: "shutdown", operationKey: "shutdown-1" }), true);
  assert.equal(isViewerCommand({ type: "reload_live" }), false);
  assert.equal(isViewerCommand({ type: "restart_agent" }), false);
  assert.equal(isViewerCommand({ type: "resubscribe_stream", streamName: "rtsp://secret@camera/live", operationKey: "command-3" }), false);
});
