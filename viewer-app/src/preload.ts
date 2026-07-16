export type PreloadIPC = {
  send(channel: string, payload: unknown): void;
  on(channel: string, handler: (...args: unknown[]) => void): void;
  removeListener(channel: string, handler?: (...args: unknown[]) => void): void;
};

export function createPreloadBridge(ipc: PreloadIPC) {
  return Object.freeze({
    reportRenderer(payload: unknown) {
      ipc.send("viewer:renderer", payload);
    },
    reportStream(payload: unknown) {
      ipc.send("viewer:stream", payload);
    },
    onCommand(handler: (command: unknown) => void) {
      if (typeof handler !== "function") return () => undefined;
      const listener = (_event: unknown, command: unknown) => handler(command);
      ipc.on("viewer:command", listener);
      return () => ipc.removeListener("viewer:command", listener);
    },
  });
}

declare const require: (name: string) => {
  contextBridge: { exposeInMainWorld(name: string, value: unknown): void };
  ipcRenderer: PreloadIPC;
};

if (typeof process !== "undefined" && process.versions.electron) {
  const { contextBridge, ipcRenderer } = require("electron");
  contextBridge.exposeInMainWorld("camstationViewer", createPreloadBridge(ipcRenderer));
}
