import { app, BrowserWindow, ipcMain, Menu } from "electron";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  ManagementConnection,
  ManagementRequestError,
  managementFailureCode,
  type ConfigDraft,
  type LeaseGrant,
  type ManagementFailureCode,
  type ViewerCommand,
  type ViewerStatus,
} from "./managementPipe.js";
import { browserWindowOptions, isNavigationAllowed, rendererStateForEvent, viewerURL } from "./navigation.js";
import { disconnectAction, liveDocumentLoadAction, reconnectDelaySeconds, setupLoadAction, startupAction } from "./viewerLifecycle.js";

const directory = path.dirname(fileURLToPath(import.meta.url));
let window: BrowserWindow | null = null;
let connection: ManagementConnection | null = null;
let lease: LeaseGrant | null = null;
let currentStatus: ViewerStatus | null = null;
let currentLiveURL = "";
let explicitShutdown = false;
let reconnectAttempt = 0;
let heartbeat: NodeJS.Timeout | null = null;
let reconnectTimer: NodeJS.Timeout | null = null;
let setupVisible = false;
let managementOutage: { readonly startedAt: number; readonly errorCode: ManagementFailureCode; reconnectCount: number } | null = null;
const rendererCommandResults = new Map<string, (succeeded: boolean) => void>();

async function run(): Promise<void> {
  if (!app.requestSingleInstanceLock()) return app.quit();
  await app.whenReady();
  Menu.setApplicationMenu(Menu.buildFromTemplate([{ label: "CamStation Viewer", submenu: [
    { label: "연결 설정", click: () => void showSetup(currentStatus) },
    { type: "separator" }, { label: "종료", click: () => app.quit() },
  ] }]));
  registerIPC();
  await connectAndShow(process.argv.includes("--autostart"));
}

function createWindow(): BrowserWindow {
  if (window) return window;
  window = new BrowserWindow(browserWindowOptions(path.join(directory, "preload.cjs"), app.isPackaged));
  hardenSession(window);
  window.once("ready-to-show", () => {
    window?.maximize();
    window?.show();
  });
  window.on("closed", () => app.quit());
  window.webContents.on("did-finish-load", () => {
    reportRenderer(rendererStateForEvent("did-finish-load"));
    reportDiagnostic({ level: "info", component: "viewer.renderer", event: "document_loaded", state: "ready" });
  });
  window.on("enter-full-screen", () => window?.webContents.send("viewer:fullscreen-changed", true));
  window.on("leave-full-screen", () => window?.webContents.send("viewer:fullscreen-changed", false));
  window.on("unresponsive", () => {
    reportRenderer(rendererStateForEvent("unresponsive"));
    reportDiagnostic({ level: "warn", component: "viewer.renderer", event: "unresponsive", state: "unresponsive" });
  });
  window.on("responsive", () => {
    reportRenderer(rendererStateForEvent("responsive"));
    reportDiagnostic({ level: "info", component: "viewer.renderer", event: "responsive", state: "ready" });
  });
  window.webContents.on("render-process-gone", () => {
    reportRenderer(rendererStateForEvent("render-process-gone"));
    reportDiagnostic({ level: "error", component: "viewer.renderer", event: "process_gone", state: "failed" });
  });
  return window;
}

async function connectAndShow(autoStartLaunch = false): Promise<void> {
  const liveVisibleBeforeConnect = hasCurrentLiveDocument();
  let attemptedConnection: ManagementConnection | null = null;
  try {
    stopHeartbeat();
    const previous = connection;
    connection = null;
    lease = null;
    previous?.close();
    const active = await ManagementConnection.connect();
    attemptedConnection = active;
    connection = active;
    active.onCommand((command) => executeViewerCommand(active, command));
    active.onDisconnect((error) => {
      if (connection !== active) return;
      observeManagementOutage(error);
      connection = null;
      lease = null;
      stopHeartbeat();
      const action = disconnectAction({
        explicitShutdown,
        retryCount: reconnectAttempt,
        liveVisible: hasCurrentLiveDocument(),
      });
      if (action === "show_service_error_and_reconnect") {
        void showSetup({
          configured: Boolean(currentStatus?.configured),
          connection: "service_unavailable",
          autoStart: false,
          leaseAvailable: false,
        }).finally(scheduleReconnect);
      } else if (action === "preserve_live_and_reconnect") {
        scheduleReconnect();
      }
    });
    const status = await active.status();
    currentStatus = status;
    const action = startupAction(status, autoStartLaunch);
    if (action === "show_setup") {
      if (liveVisibleBeforeConnect) {
        if (connection === active) connection = null;
        active.close();
        scheduleReconnect();
        return;
      }
      await showSetup(status);
      if (status.configured) scheduleReconnect();
      return;
    }
    if (action === "quit") {
      if (!liveVisibleBeforeConnect) return app.quit();
      if (connection === active) connection = null;
      active.close();
      scheduleReconnect();
      return;
    }
    await showLive(status, liveVisibleBeforeConnect);
    reconnectAttempt = 0;
  } catch (error) {
    if (!attemptedConnection) observeManagementOutage(error);
    if (attemptedConnection && connection === attemptedConnection) {
      stopHeartbeat();
      connection = null;
      lease = null;
      attemptedConnection.close();
    }
    if (!hasCurrentLiveDocument() && !liveVisibleBeforeConnect) {
      try {
        await showSetup({
          configured: Boolean(currentStatus?.configured),
          connection: "service_unavailable",
          autoStart: false,
          leaseAvailable: false,
        });
      } catch {
        // Reconnect remains bounded even when the local setup document cannot load.
      }
    }
    scheduleReconnect();
  }
}

async function showLive(status: ViewerStatus, preserveExistingDocument = false): Promise<void> {
  if (!connection || !status.config) return showSetup(status);
  if (!lease) {
    try {
      lease = await connection.acquireLease();
      reportDiagnostic({ level: "info", component: "viewer.main", event: "lease_acquired", state: "running" });
    } catch (error) {
      if (error instanceof ManagementRequestError && error.code === "lease_busy" && !preserveExistingDocument) {
        return app.quit();
      }
      throw error;
    }
  }
  const nextLiveURL = viewerURL(status.config.serverUrl);
  const loadAction = liveDocumentLoadAction({
    liveVisible: preserveExistingDocument && hasCurrentLiveDocument(),
    currentURL: currentLiveURL,
    nextURL: nextLiveURL,
  });
  cancelScheduledReconnect();
  startHeartbeat();
  currentLiveURL = nextLiveURL;
  connection.reportViewer(lease.leaseId, "running");
  setupVisible = false;
  if (loadAction === "preserve") {
    reportRenderer("ready");
    reportDiagnostic({ level: "info", component: "viewer.main", event: "lease_reacquired", state: "running" });
    reportManagementRecovery();
    return;
  }
  reportDiagnostic({ level: "info", component: "viewer.main", event: "live_load_started", state: "running" });
  await createWindow().loadURL(currentLiveURL);
  reportDiagnostic({ level: "info", component: "viewer.main", event: "live_loaded", state: "running" });
  reportManagementRecovery();
}

async function showSetup(status: ViewerStatus | null): Promise<void> {
  stopHeartbeat();
  currentStatus = status;
  currentLiveURL = "";
  if (setupLoadAction(setupVisible) === "preserve") return;
  setupVisible = true;
  await createWindow().loadFile(path.join(directory, "../assets/setup.html"));
}

function registerIPC(): void {
  ipcMain.on("viewer:renderer", (_event, payload: unknown) => reportRenderer((payload as { state?: string })?.state ?? "ready"));
  ipcMain.on("viewer:stream", (_event, payload: unknown) => lease && connection?.reportStream(lease.leaseId, payload));
  ipcMain.on("viewer:diagnostic", (_event, payload: unknown) => reportDiagnostic(payload));
  ipcMain.on("viewer:command-result", (event, payload: unknown) => {
    if (!window || event.sender !== window.webContents || !payload || typeof payload !== "object") return;
    const result = payload as Record<string, unknown>;
    if (typeof result.operationKey !== "string" || typeof result.succeeded !== "boolean") return;
    const resolve = rendererCommandResults.get(result.operationKey);
    if (!resolve) return;
    rendererCommandResults.delete(result.operationKey);
    resolve(result.succeeded);
  });
  ipcMain.handle("viewer:setup-state", () => currentStatus);
  ipcMain.handle("viewer:retry-connection", async () => {
    await connectAndShow(false);
    return currentStatus;
  });
  ipcMain.handle("viewer:set-fullscreen", (event, fullscreen: unknown) => {
    if (!window || event.sender !== window.webContents) return false;
    window.setFullScreen(fullscreen === true);
    return window.isFullScreen();
  });
  ipcMain.handle("viewer:configure", async (_event, draft: ConfigDraft) => {
    if (!connection) return { ok: false, errorCode: "service_unavailable" };
    try {
      const status = await connection.configure(draft);
      currentStatus = status;
      await showLive(status);
      return { ok: true, status };
    } catch (error) {
      return { ok: false, errorCode: error instanceof ManagementRequestError ? error.code : "storage_failed" };
    }
  });
}

async function executeViewerCommand(active: ManagementConnection, command: ViewerCommand): Promise<void> {
  const activeLease = lease;
  if (!activeLease || connection !== active) return;
  let succeeded = false;
  let errorCode = "viewer_command_failed";
  let relaunch = false;
  reportDiagnostic({
    level: "info", component: "viewer.main", event: "command_started", state: "running",
    correlationId: command.operationKey,
  });
  try {
    switch (command.type) {
      case "reload_live":
        if (!window || !currentLiveURL) throw new Error("live window is unavailable");
        await window.loadURL(currentLiveURL);
        break;
      case "resubscribe_stream":
        if (!window || !currentLiveURL) throw new Error("live renderer is unavailable");
        if (!await sendRendererCommand(window, command)) {
          errorCode = "renderer_failed";
          throw new Error("renderer rejected Viewer command");
        }
        break;
      case "restart_viewer":
        relaunch = true;
        break;
    }
    succeeded = true;
  } catch {
    succeeded = false;
  }
  try {
    await active.reportCommandResult(activeLease.leaseId, command.operationKey, succeeded, succeeded ? undefined : errorCode);
  } catch {
    return;
  }
  reportDiagnostic({
    level: succeeded ? "info" : "error", component: "viewer.main",
    event: succeeded ? "command_succeeded" : "command_failed",
    state: succeeded ? "running" : "failed", correlationId: command.operationKey,
    ...(succeeded ? {} : { errorCode }),
  });
  if (succeeded && relaunch) {
    explicitShutdown = true;
    app.relaunch();
    app.exit(0);
  }
}

function sendRendererCommand(target: BrowserWindow, command: Extract<ViewerCommand, { readonly type: "resubscribe_stream" }>): Promise<boolean> {
  if (rendererCommandResults.has(command.operationKey)) return Promise.resolve(false);
  return new Promise<boolean>((resolve) => {
    const timer = setTimeout(() => {
      rendererCommandResults.delete(command.operationKey);
      resolve(false);
    }, 10_000);
    rendererCommandResults.set(command.operationKey, (succeeded) => {
      clearTimeout(timer);
      resolve(succeeded);
    });
    target.webContents.send("viewer:command", command);
  });
}

function reportRenderer(state: string): void {
  if (lease) connection?.reportRenderer(lease.leaseId, { state, source: "viewer" });
}

function reportDiagnostic(payload: unknown): void {
  if (lease) connection?.reportDiagnostic(lease.leaseId, payload);
}

function observeManagementOutage(error: unknown): void {
  if (!managementOutage) {
    managementOutage = { startedAt: Date.now(), errorCode: managementFailureCode(error), reconnectCount: 0 };
    return;
  }
  managementOutage.reconnectCount = Math.min(100, managementOutage.reconnectCount + 1);
}

function reportManagementRecovery(): void {
  const outage = managementOutage;
  if (!outage) return;
  managementOutage = null;
  reportDiagnostic({
    level: "warn",
    component: "viewer.control",
    event: "management_recovered",
    state: "running",
    errorCode: outage.errorCode,
    durationMs: Math.min(30 * 60_000, Math.max(0, Date.now() - outage.startedAt)),
    reconnectCount: outage.reconnectCount,
  });
}

function startHeartbeat(): void {
  stopHeartbeat();
  const activeConnection = connection;
  const activeLease = lease;
  if (!activeConnection || !activeLease) return;
  let inFlight = false;
  const pulse = () => {
    if (inFlight || connection !== activeConnection || lease !== activeLease) return;
    inFlight = true;
    void activeConnection.heartbeat(activeLease.leaseId).catch(() => undefined).finally(() => {
      inFlight = false;
    });
  };
  heartbeat = setInterval(pulse, activeLease.heartbeatSeconds * 1_000);
  pulse();
}

function stopHeartbeat(): void {
  if (heartbeat) clearInterval(heartbeat);
  heartbeat = null;
}

function scheduleReconnect(): void {
  if (reconnectTimer || explicitShutdown) return;
  const delay = reconnectDelaySeconds(reconnectAttempt++) * 1_000;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    void connectAndShow(false);
  }, delay);
}

function cancelScheduledReconnect(): void {
  if (reconnectTimer) clearTimeout(reconnectTimer);
  reconnectTimer = null;
  reconnectAttempt = 0;
}

function hasCurrentLiveDocument(): boolean {
  if (!window || window.isDestroyed() || setupVisible || !currentLiveURL) return false;
  try {
    return window.webContents.getURL() === currentLiveURL;
  } catch {
    return false;
  }
}

function hardenSession(target: BrowserWindow): void {
  target.webContents.on("will-navigate", (event, candidate) => {
    if (!currentLiveURL || !isNavigationAllowed(candidate, currentLiveURL)) event.preventDefault();
  });
  target.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  target.webContents.session.setPermissionRequestHandler((_contents, _permission, callback) => callback(false));
  target.webContents.session.setPermissionCheckHandler(() => false);
  target.webContents.session.on("will-download", (event) => event.preventDefault());
  if (app.isPackaged) target.webContents.on("before-input-event", (event, input) => {
    if (input.key === "F12" || (input.control && input.shift && input.key.toLowerCase() === "i")) event.preventDefault();
  });
}

app.on("window-all-closed", () => app.quit());
app.once("before-quit", () => {
  explicitShutdown = true;
  stopHeartbeat();
  cancelScheduledReconnect();
  if (lease) connection?.release(lease.leaseId);
  connection?.close();
  for (const resolve of rendererCommandResults.values()) resolve(false);
  rendererCommandResults.clear();
});
process.on("SIGBREAK", () => app.quit());
process.on("SIGTERM", () => app.quit());
run().catch(() => app.exit(1));
