import { contextBridge, ipcRenderer } from 'electron';

contextBridge.exposeInMainWorld('electronAPI', {
  getSettings: (): Promise<{ serverUrl: string; fullscreenOnStart: boolean }> =>
    ipcRenderer.invoke('get-settings'),

  saveSettings: (s: { serverUrl: string; fullscreenOnStart: boolean }): Promise<void> =>
    ipcRenderer.invoke('save-settings', s),

  testConnection: (url: string): Promise<{ ok: boolean; error?: string }> =>
    ipcRenderer.invoke('test-connection', url),

  launchViewer: (url: string): Promise<void> =>
    ipcRenderer.invoke('launch-viewer', url),

  getVersion: (): Promise<string> =>
    ipcRenderer.invoke('get-version'),
});
