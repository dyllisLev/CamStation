import { app, BrowserWindow, ipcMain, Tray, Menu, nativeImage } from 'electron';
import type { MenuItemConstructorOptions } from 'electron';
import path from 'path';
import fs from 'fs';
import http from 'http';
import https from 'https';
import os from 'os';
import { randomUUID } from 'crypto';
import { spawn } from 'child_process';
import { checkForUpdates } from './updater';
import { buildWindowsPortableRestartScript } from './updaterCore';
import { buildViewerUrl, normalizeServerUrl, shouldRestrictViewerNavigation } from './viewerNavigation';

interface Settings {
  serverUrl: string;
  fullscreenOnStart: boolean;
  clientId?: string;
  clientName?: string;
}

interface ViewerIdentity {
  clientId: string;
  name: string;
  appVersion: string;
  platform: string;
  hostname: string;
  pid: number;
  startedAt: number;
}

const startedAt = Date.now() / 1000;

function getSettingsPath(): string {
  return path.join(app.getPath('userData'), 'settings.json');
}

function loadSettings(): Settings {
  try {
    const settings = JSON.parse(fs.readFileSync(getSettingsPath(), 'utf8')) as Settings;
    return { ...settings, serverUrl: settings.serverUrl ?? '', fullscreenOnStart: settings.fullscreenOnStart ?? false };
  } catch {
    return { serverUrl: '', fullscreenOnStart: false };
  }
}

function persistSettings(settings: Settings): void {
  fs.writeFileSync(getSettingsPath(), JSON.stringify(settings, null, 2));
}

function getViewerIdentity(): ViewerIdentity {
  const settings = loadSettings();
  if (!settings.clientId) {
    settings.clientId = `camviewer-${randomUUID()}`;
    persistSettings(settings);
  }
  return {
    clientId: settings.clientId,
    name: settings.clientName || os.hostname() || 'CamViewer',
    appVersion: app.getVersion(),
    platform: process.platform,
    hostname: os.hostname(),
    pid: process.pid,
    startedAt,
  };
}

function restartViewerApp(): { ok: boolean; message?: string } {
  if (process.platform === 'win32') {
    const exePath = process.env.PORTABLE_EXECUTABLE_FILE ?? process.execPath;
    const batPath = path.join(app.getPath('temp'), 'camviewer-restart.bat');
    fs.writeFileSync(batPath, buildWindowsPortableRestartScript({ exePath }), 'latin1');
    spawn('cmd.exe', ['/c', 'start', '', batPath], {
      detached: true,
      stdio: 'ignore',
      windowsHide: true,
    }).unref();
    setTimeout(() => app.exit(0), 300);
    return { ok: true, message: 'restarting via portable launcher' };
  }
  app.relaunch();
  setTimeout(() => app.exit(0), 500);
  return { ok: true, message: 'restarting' };
}

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let updateTimer: ReturnType<typeof setInterval> | null = null;
let crashCount = 0;
let activeViewerServerUrl = '';

function loadSetupPage(): void {
  activeViewerServerUrl = '';
  if (!mainWindow) return;
  if (mainWindow.isFullScreen()) mainWindow.setFullScreen(false);
  mainWindow.loadFile(path.join(__dirname, '../src/setup.html'));
}

function loadViewerPage(serverUrl: string): void {
  if (!mainWindow) return;
  const normalizedServerUrl = normalizeServerUrl(serverUrl);
  activeViewerServerUrl = normalizedServerUrl;
  mainWindow.loadURL(buildViewerUrl(normalizedServerUrl));
  startUpdateChecks(normalizedServerUrl);
}

function showLiveOrSetup(): void {
  const settings = loadSettings();
  if (!settings.serverUrl) {
    loadSetupPage();
    return;
  }
  if (settings.fullscreenOnStart) mainWindow?.setFullScreen(true);
  loadViewerPage(settings.serverUrl);
}

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

  setupWatchdog();
  setupEscKey();
  setupViewerNavigationGuard();
  setupTray();
  setupAppMenu();

  const settings = loadSettings();
  if (!settings.serverUrl) {
    loadSetupPage();
  } else {
    if (settings.fullscreenOnStart) mainWindow.setFullScreen(true);
    loadViewerPage(settings.serverUrl);
  }
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

function setupViewerNavigationGuard(): void {
  if (!mainWindow) return;

  const redirectToViewer = () => {
    if (!activeViewerServerUrl) return;
    mainWindow?.loadURL(buildViewerUrl(activeViewerServerUrl));
  };

  mainWindow.webContents.on('will-navigate', (event, targetUrl) => {
    if (!shouldRestrictViewerNavigation(targetUrl, activeViewerServerUrl)) return;
    event.preventDefault();
    redirectToViewer();
  });

  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (shouldRestrictViewerNavigation(url, activeViewerServerUrl)) {
      redirectToViewer();
      return { action: 'deny' };
    }
    return { action: 'allow' };
  });
}

function setupAppMenu(): void {
  const template: MenuItemConstructorOptions[] = [
    {
      label: 'CamViewer',
      submenu: [
        { label: '라이브 화면', click: () => showLiveOrSetup() },
        { label: '서버 정보 수정', click: () => loadSetupPage() },
        { type: 'separator' },
        {
          label: '전체화면 전환',
          accelerator: 'F11',
          click: () => {
            if (!mainWindow) return;
            mainWindow.setFullScreen(!mainWindow.isFullScreen());
          },
        },
        { type: 'separator' },
        { label: '종료', click: () => app.quit() },
      ],
    },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function setupTray(): void {
  tray = new Tray(nativeImage.createEmpty());
  tray.setToolTip('CamViewer');
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: '창 보기', click: () => mainWindow?.show() },
      {
        label: '서버 정보 수정',
        click: () => loadSetupPage(),
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
  persistSettings({
    ...settings,
    serverUrl: normalizeServerUrl(settings.serverUrl),
  });
});

ipcMain.handle(
  'test-connection',
  (_, url: string): Promise<{ ok: boolean; error?: string }> =>
    new Promise(resolve => {
      const normalizedUrl = normalizeServerUrl(url);
      const mod = normalizedUrl.startsWith('https') ? https : http;
      const req = mod.get(`${normalizedUrl}/api/status`, { timeout: 5000 }, res => {
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
  loadViewerPage(url);
});

ipcMain.handle('get-version', (): string => app.getVersion());

ipcMain.handle('get-viewer-identity', (): ViewerIdentity => getViewerIdentity());

ipcMain.handle('viewer-action', (_, action: string): { ok: boolean; message?: string } => {
  if (action === 'ping') return { ok: true, message: 'pong' };
  if (action === 'reload_page' || action === 'refresh_streams') {
    mainWindow?.webContents.reloadIgnoringCache();
    return { ok: true, message: 'reloaded' };
  }
  if (action === 'restart_app') {
    return restartViewerApp();
  }
  return { ok: false, message: `unknown action: ${action}` };
});

app.whenReady().then(createWindow);
app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit(); });
app.on('activate', () => { if (!mainWindow) createWindow(); });
