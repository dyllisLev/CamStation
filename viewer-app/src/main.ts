import { app, BrowserWindow, ipcMain } from "electron";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { AgentConnection, parseLaunchIdentity } from "./agentPipe.js";
import { browserWindowOptions, isNavigationAllowed, rendererStateForEvent, viewerURL } from "./navigation.js";
import { disconnectExitCode } from "./viewerLifecycle.js";

const directory = path.dirname(fileURLToPath(import.meta.url));

async function run(): Promise<void> {
  if (!app.requestSingleInstanceLock()) {
    app.quit();
    return;
  }
  await app.whenReady();
  const identity = parseLaunchIdentity(process.argv.slice(1));
  const agent = await AgentConnection.connect(identity);
  const liveURL = viewerURL(agent.serverURL);
  const window = new BrowserWindow(browserWindowOptions(path.join(directory, "preload.cjs"), app.isPackaged));
  let explicitShutdown = false;

  hardenSession(window, liveURL);
  ipcMain.on("viewer:renderer", (event) => {
    if (event.sender === window.webContents) agent.reportRenderer({ state: "ready", source: "renderer" });
  });
  ipcMain.on("viewer:stream", (event, payload: unknown) => {
    if (event.sender === window.webContents) agent.reportStream(payload);
  });
  window.webContents.on("did-finish-load", () => agent.reportRenderer({ state: rendererStateForEvent("did-finish-load"), source: "host" }));
  window.on("unresponsive", () => agent.reportRenderer({ state: rendererStateForEvent("unresponsive"), source: "host" }));
  window.on("responsive", () => agent.reportRenderer({ state: rendererStateForEvent("responsive"), source: "host" }));
  window.webContents.on("render-process-gone", () => agent.reportRenderer({ state: rendererStateForEvent("render-process-gone"), source: "host" }));
  const unsubscribe = agent.onCommand((command) => {
    switch (command.type) {
    case "reload_live":
      return window.loadURL(liveURL).then(() => undefined);
    case "resubscribe_stream":
      window.webContents.send("viewer:command", command);
      break;
    case "shutdown":
      break;
    }
  });
  const unsubscribeShutdown = agent.onShutdown(() => {
    explicitShutdown = true;
    app.quit();
  });
  const unsubscribeDisconnect = agent.onDisconnect(() => {
    const exitCode = disconnectExitCode(explicitShutdown);
    if (exitCode !== null) app.exit(exitCode);
  });
  window.once("ready-to-show", () => window.show());
  window.on("closed", () => app.quit());
  app.once("before-quit", () => {
    explicitShutdown = true;
    unsubscribe();
    unsubscribeShutdown();
    unsubscribeDisconnect();
    agent.close();
  });
  await window.loadURL(liveURL);
}

function hardenSession(window: BrowserWindow, liveURL: string): void {
  window.webContents.on("will-navigate", (event, candidate) => {
    if (!isNavigationAllowed(candidate, liveURL)) event.preventDefault();
  });
  window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  window.webContents.session.setPermissionRequestHandler((_contents, _permission, callback) => callback(false));
  window.webContents.session.setPermissionCheckHandler(() => false);
  window.webContents.session.on("will-download", (event) => event.preventDefault());
  if (app.isPackaged) {
    window.webContents.on("before-input-event", (event, input) => {
      if (input.key === "F12" || (input.control && input.shift && input.key.toLowerCase() === "i")) event.preventDefault();
    });
    window.webContents.on("devtools-opened", () => window.webContents.closeDevTools());
  }
}

app.on("window-all-closed", () => app.quit());
process.on("SIGBREAK", () => app.quit());
process.on("SIGTERM", () => app.quit());
run().catch(() => app.exit(1));
