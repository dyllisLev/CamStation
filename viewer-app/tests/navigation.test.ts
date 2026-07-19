import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import {
  browserWindowOptions,
  isNavigationAllowed,
  permissionAllowed,
  rendererStateForEvent,
  viewerURL,
} from "../src/navigation.ts";

test("viewer URL always targets the 2.0 live route", () => {
  assert.equal(viewerURL("http://10.0.0.5:18080"), "http://10.0.0.5:18080/live?viewer=1");
  assert.equal(viewerURL("https://camstation.local/"), "https://camstation.local/live?viewer=1");
  assert.throws(() => viewerURL("file:///tmp/index.html"));
  assert.throws(() => viewerURL("http://user:secret@10.0.0.5:18080"));
});

test("renderer lifecycle reports stable bounded states", () => {
  assert.equal(rendererStateForEvent("did-finish-load"), "not_ready");
  assert.equal(rendererStateForEvent("unresponsive"), "unresponsive");
  assert.equal(rendererStateForEvent("responsive"), "not_ready");
  assert.equal(rendererStateForEvent("render-process-gone"), "failed");
  assert.equal(rendererStateForEvent("unexpected"), "not_ready");
});

test("BrowserWindow uses the hardened renderer boundary", () => {
  const options = browserWindowOptions("C:\\CamStation\\preload.js", true);
  assert.equal(options.autoHideMenuBar, false);
  assert.deepEqual(options.webPreferences, {
    preload: "C:\\CamStation\\preload.js",
    nodeIntegration: false,
    contextIsolation: true,
    sandbox: true,
    webSecurity: true,
    devTools: false,
  });
});

test("Viewer reveals its window once a document has finished loading", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/main.ts"), "utf8");
  assert.match(source, /webContents\.on\("did-finish-load", \(\) => \{\s*window\?\.show\(\);/u);
});

test("Viewer owns fullscreen through native BrowserWindow IPC", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/main.ts"), "utf8");
  assert.match(source, /ipcMain\.handle\("viewer:set-fullscreen"/u);
  assert.match(source, /window\.on\("enter-full-screen"/u);
  assert.match(source, /window\.on\("leave-full-screen"/u);
});

test("setup document loads its renderer as an ES module", async () => {
  const document = await readFile(path.resolve(import.meta.dirname, "../assets/setup.html"), "utf8");
  assert.match(document, /<script type="module" src="\.\.\/build\/setupRenderer\.js"><\/script>/u);
});

test("navigation and permissions are denied outside the exact live page", () => {
  const live = "http://10.0.0.5:18080/live?viewer=1";
  assert.equal(isNavigationAllowed(live, live), true);
  assert.equal(isNavigationAllowed("http://10.0.0.5:18080/recordings?viewer=1", live), true);
  assert.equal(isNavigationAllowed("http://10.0.0.5:18080/recordings", live), false);
  assert.equal(isNavigationAllowed("http://10.0.0.5:18080/settings", live), false);
  assert.equal(isNavigationAllowed("https://example.com/", live), false);
  assert.equal(permissionAllowed("media"), false);
});
