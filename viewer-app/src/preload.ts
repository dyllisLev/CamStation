export type PreloadIPC = {
  send(channel: string, payload: unknown): void;
  invoke?(channel: string, ...args: unknown[]): Promise<unknown>;
  on(channel: string, handler: (...args: unknown[]) => void): void;
  removeListener(channel: string, handler?: (...args: unknown[]) => void): void;
};

export function createPreloadBridge(ipc: PreloadIPC) {
  const commandHandlers = new Set<(command: unknown) => boolean | Promise<boolean>>();
  const commandListener = (_event: unknown, command: unknown) => {
    const operationKey = command && typeof command === "object" ? (command as Record<string, unknown>).operationKey : undefined;
    void Promise.all([...commandHandlers].map(async (handler) => {
      try {
        return await handler(command);
      } catch {
        return false;
      }
    })).then((results) => {
      ipc.send("viewer:command-result", { operationKey, succeeded: results.some(Boolean) });
    });
  };
  return Object.freeze({
    getSetupState() {
      return ipc.invoke?.("viewer:setup-state");
    },
    saveConfiguration(draft: unknown) {
      return ipc.invoke?.("viewer:configure", draft);
    },
    retryConnection() {
      return ipc.invoke?.("viewer:retry-connection");
    },
    setFullscreen(fullscreen: boolean) {
      return ipc.invoke?.("viewer:set-fullscreen", Boolean(fullscreen));
    },
    reportRenderer(payload: unknown) {
      ipc.send("viewer:renderer", payload);
    },
    reportStream(payload: unknown) {
      ipc.send("viewer:stream", payload);
    },
    reportDiagnostic(payload: unknown) {
      ipc.send("viewer:diagnostic", payload);
    },
    onCommand(handler: (command: unknown) => boolean | Promise<boolean>) {
      if (typeof handler !== "function") return () => undefined;
      if (commandHandlers.size === 0) ipc.on("viewer:command", commandListener);
      commandHandlers.add(handler);
      return () => {
        commandHandlers.delete(handler);
        if (commandHandlers.size === 0) ipc.removeListener("viewer:command", commandListener);
      };
    },
    onFullscreenChange(handler: (fullscreen: boolean) => void) {
      const listener = (_event: unknown, fullscreen: unknown) => {
        if (typeof fullscreen === "boolean") handler(fullscreen);
      };
      ipc.on("viewer:fullscreen-changed", listener);
      return () => ipc.removeListener("viewer:fullscreen-changed", listener);
    },
  });
}

export function startRendererHeartbeat(ipc: PreloadIPC): () => void {
  const pulse = () => ipc.send("viewer:renderer", { state: "ready" });
  pulse();
  const timer = setInterval(pulse, 5_000);
  return () => clearInterval(timer);
}

declare const require: (name: string) => {
  contextBridge: { exposeInMainWorld(name: string, value: unknown): void };
  ipcRenderer: PreloadIPC;
};

if (typeof process !== "undefined" && process.versions.electron) {
  const { contextBridge, ipcRenderer } = require("electron");
  contextBridge.exposeInMainWorld("camstationViewer", createPreloadBridge(ipcRenderer));
  startRendererHeartbeat(ipcRenderer);
}
