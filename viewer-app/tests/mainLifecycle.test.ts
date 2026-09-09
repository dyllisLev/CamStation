import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { runInNewContext } from "node:vm";
import { isNavigationAllowed } from "../src/navigation.ts";
import { disconnectAction, liveDocumentLoadAction, reconnectDelaySeconds, setupLoadAction, startupAction } from "../src/viewerLifecycle.ts";

test("direct launch decides between setup, quiet exit, and live Viewer", () => {
  assert.equal(startupAction({ configured: false, autoStart: true, leaseAvailable: true }, false), "show_setup");
  assert.equal(startupAction({ configured: true, autoStart: false, leaseAvailable: true }, true), "quit");
  assert.equal(startupAction({ configured: true, autoStart: true, leaseAvailable: false }, false), "quit");
  assert.equal(startupAction({ configured: true, autoStart: true, leaseAvailable: true, connection: "offline" }, false), "show_setup");
  assert.equal(startupAction({ configured: true, autoStart: true, leaseAvailable: true }, false), "acquire_lease");
});

test("service disconnect is recoverable unless Viewer is explicitly closing", () => {
  assert.equal(disconnectAction({ explicitShutdown: false, retryCount: 0, liveVisible: false }), "show_service_error_and_reconnect");
  assert.equal(disconnectAction({ explicitShutdown: false, retryCount: 0, liveVisible: true }), "preserve_live_and_reconnect");
  assert.equal(disconnectAction({ explicitShutdown: true, retryCount: 0, liveVisible: true }), "quit");
});

test("service reconnect retries use the bounded 1/2/5/10/30-second sequence", () => {
  assert.deepEqual([0, 1, 2, 3, 4, 5, 6].map(reconnectDelaySeconds), [1, 2, 5, 10, 30, 30, 30]);
});

test("offline reconnect preserves an already visible setup form", () => {
  assert.equal(setupLoadAction(false), "load");
  assert.equal(setupLoadAction(true), "preserve");
});

test("management reconnect preserves only the same verified live document", () => {
  assert.equal(liveDocumentLoadAction({
    liveVisible: true,
    currentURL: "http://camstation/live?viewer=1",
    nextURL: "http://camstation/live?viewer=1",
  }), "preserve");
  assert.equal(liveDocumentLoadAction({
    liveVisible: true,
    currentURL: "http://old/live?viewer=1",
    nextURL: "http://new/live?viewer=1",
  }), "load");
  assert.equal(liveDocumentLoadAction({
    liveVisible: false,
    currentURL: "http://camstation/live?viewer=1",
    nextURL: "http://camstation/live?viewer=1",
  }), "load");
});

test("management disconnect and lease recovery preserve the allowed recordings document", async () => {
  const source = await readFile(new URL("../src/main.ts", import.meta.url), "utf8");
  // Exercise the actual main-process visibility predicate with a BrowserWindow stub.
  const predicateSource = source.slice(
    source.indexOf("function hasCurrentLiveDocument()"),
    source.indexOf("function hardenSession("),
  ).replace("(): boolean", "()");
  const currentLiveURL = "http://camstation/live?viewer=1";
  for (const [documentURL, expectedVisible] of [
    [currentLiveURL, true],
    ["http://camstation/recordings?viewer=1", true],
    ["http://camstation/recordings", false],
    ["http://camstation/settings?viewer=1", false],
    ["http://other/recordings?viewer=1", false],
    ["file:///setup.html", false],
  ] as const) {
    const isVisible = runInNewContext(`${predicateSource}\nhasCurrentLiveDocument`, {
      window: { isDestroyed: () => false, webContents: { getURL: () => documentURL } },
      setupVisible: false,
      currentLiveURL,
      isNavigationAllowed,
    }) as () => boolean;
    const liveVisible = isVisible();
    assert.equal(liveVisible, expectedVisible, documentURL);
    assert.equal(disconnectAction({ explicitShutdown: false, retryCount: 0, liveVisible }),
      expectedVisible ? "preserve_live_and_reconnect" : "show_service_error_and_reconnect", documentURL);
    assert.equal(liveDocumentLoadAction({ liveVisible, currentURL: currentLiveURL, nextURL: currentLiveURL }),
      expectedVisible ? "preserve" : "load", documentURL);
    assert.equal(liveDocumentLoadAction({ liveVisible, currentURL: currentLiveURL, nextURL: "http://new/live?viewer=1" }), "load");
  }
});
