import assert from "node:assert/strict";
import test from "node:test";
import { disconnectAction, reconnectDelaySeconds, setupLoadAction, startupAction } from "../src/viewerLifecycle.ts";

test("direct launch decides between setup, quiet exit, and live Viewer", () => {
  assert.equal(startupAction({ configured: false, autoStart: true, leaseAvailable: true }, false), "show_setup");
  assert.equal(startupAction({ configured: true, autoStart: false, leaseAvailable: true }, true), "quit");
  assert.equal(startupAction({ configured: true, autoStart: true, leaseAvailable: false }, false), "quit");
  assert.equal(startupAction({ configured: true, autoStart: true, leaseAvailable: true, connection: "offline" }, false), "show_setup");
  assert.equal(startupAction({ configured: true, autoStart: true, leaseAvailable: true }, false), "acquire_lease");
});

test("service disconnect is recoverable unless Viewer is explicitly closing", () => {
  assert.equal(disconnectAction({ explicitShutdown: false, retryCount: 0 }), "show_service_error_and_reconnect");
  assert.equal(disconnectAction({ explicitShutdown: true, retryCount: 0 }), "quit");
});

test("service reconnect retries use the bounded 1/2/5/10/30-second sequence", () => {
  assert.deepEqual([0, 1, 2, 3, 4, 5, 6].map(reconnectDelaySeconds), [1, 2, 5, 10, 30, 30, 30]);
});

test("offline reconnect preserves an already visible setup form", () => {
  assert.equal(setupLoadAction(false), "load");
  assert.equal(setupLoadAction(true), "preserve");
});
