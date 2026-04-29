import { app, BrowserWindow, ipcMain, Tray, Menu, nativeImage } from 'electron';
import path from 'path';
import fs from 'fs';
import http from 'http';
import https from 'https';
import { checkForUpdates } from './updater';

interface Settings {
  serverUrl: string;
  fullscreenOnStart: boolean;
}

function getSettingsPath(): string {
  return path.join(app.getPath('userData'), 'settings.json');
}

function loadSettings(): Settings {
  try {
    return JSON.parse(fs.readFileSync(getSettingsPath(), 'utf8')) as Settings;
  } catch {
    return { serverUrl: '', fullscreenOnStart: false };
  }
}

function persistSettings(settings: Settings): void {
  fs.writeFileSync(getSettingsPath(), JSON.stringify(settings, null, 2));
}

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let updateTimer: ReturnType<typeof setInterval> | null = null;
let crashCount = 0;

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 640,
    minHeight: 360,
    backgroundColor: '#111111',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
    title: 'CamViewer',
  });

  const settings = loadSettings();
  if (!settings.serverUrl) {
    mainWindow.loadFile(path.join(__dirname, '../src/setup.html'));
  } else {
    if (settings.fullscreenOnStart) mainWindow.setFullScreen(true);
    mainWindow.loadURL(`${settings.serverUrl}/viewer`);
    startUpdateChecks(settings.serverUrl);
  }

  setupWatchdog();
  setupEscKey();
  setupTray();
}

function setupWatchdog(): void {
  if (!mainWindow) return;

  mainWindow.webContents.on('render-process-gone', (_, details) => {
    if (details.reason === 'clean-exit') return;
    crashCount++;
    if (crashCount >= 3) {
      crashCount = 0;
      mainWindow?.close();
      mainWindow = null;
      setTimeout(createWindow, 3000);
    } else {
      setTimeout(() => mainWindow?.webContents.reload(), 3000);
    }
  });

  mainWindow.webContents.on('unresponsive', () => {
    setTimeout(() => {
      mainWindow?.webContents.forcefullyCrashRenderer();
    }, 5000);
  });

  mainWindow.webContents.on('did-finish-load', () => {
    crashCount = 0;
  });
}

function setupEscKey(): void {
  if (!mainWindow) return;
  mainWindow.webContents.on('before-input-event', (_, input) => {
    if (input.type === 'keyDown' && input.key === 'Escape' && mainWindow?.isFullScreen()) {
      mainWindow.setFullScreen(false);
    }
  });
}

function setupTray(): void {
  tray = new Tray(nativeImage.createEmpty());
  tray.setToolTip('CamViewer');
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: '창 보기', click: () => mainWindow?.show() },
      {
        label: '서버 설정',
        click: () => mainWindow?.loadFile(path.join(__dirname, '../src/setup.html')),
      },
      { type: 'separator' },
      { label: '종료', click: () => app.quit() },
    ]),
  );
  tray.on('double-click', () => mainWindow?.show());
}

function startUpdateChecks(serverUrl: string): void {
  if (updateTimer) clearInterval(updateTimer);
  checkForUpdates(serverUrl);
  updateTimer = setInterval(() => checkForUpdates(serverUrl), 60 * 60 * 1000);
}

// IPC 핸들러
ipcMain.handle('get-settings', (): Settings => loadSettings());

ipcMain.handle('save-settings', (_, settings: Settings): void => {
  persistSettings(settings);
});

ipcMain.handle(
  'test-connection',
  (_, url: string): Promise<{ ok: boolean; error?: string }> =>
    new Promise(resolve => {
      const mod = url.startsWith('https') ? https : http;
      const req = mod.get(`${url}/api/status`, { timeout: 5000 }, res => {
        res.resume();
        resolve(
          res.statusCode && res.statusCode < 500
            ? { ok: true }
            : { ok: false, error: `서버 오류 ${res.statusCode}` },
        );
      });
      req.on('error', err => resolve({ ok: false, error: err.message }));
      req.on('timeout', () => { req.destroy(); resolve({ ok: false, error: '연결 시간 초과' }); });
    }),
);

ipcMain.handle('launch-viewer', (_, url: string): void => {
  if (!mainWindow) return;
  const settings = loadSettings();
  if (settings.fullscreenOnStart) mainWindow.setFullScreen(true);
  mainWindow.loadURL(`${url}/viewer`);
  startUpdateChecks(url);
});

ipcMain.handle('get-version', (): string => app.getVersion());

app.whenReady().then(createWindow);
app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit(); });
app.on('activate', () => { if (!mainWindow) createWindow(); });
