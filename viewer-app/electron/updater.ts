import { app, Notification } from 'electron';
import http from 'http';
import https from 'https';
import fs from 'fs';
import path from 'path';
import { exec } from 'child_process';
import { buildWindowsPortableUpdateScript, normalizeVersion, shouldInstallUpdate } from './updaterCore';

function get(url: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const mod = url.startsWith('https') ? https : http;
    const req = mod.get(url, { timeout: 10_000 }, res => {
      const chunks: Buffer[] = [];
      res.on('data', c => chunks.push(Buffer.from(c)));
      res.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
    });
    req.on('error', reject);
    req.on('timeout', () => { req.destroy(); reject(new Error('timeout')); });
  });
}

function downloadFile(url: string, dest: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const mod = url.startsWith('https') ? https : http;
    const file = fs.createWriteStream(dest);
    mod.get(url, res => {
      res.pipe(file);
      file.on('finish', () => { file.close(); resolve(); });
      file.on('error', err => { fs.unlink(dest, () => {}); reject(err); });
    }).on('error', err => { fs.unlink(dest, () => {}); reject(err); });
  });
}

let pendingVersion: string | null = null;

export function shouldInstallViewerUpdate(
  currentVersion: string,
  serverVersion: string | null | undefined,
  existingPendingVersion: string | null,
): boolean {
  return shouldInstallUpdate(serverVersion, currentVersion, existingPendingVersion);
}

export function resolvePortableExePath(
  env: NodeJS.ProcessEnv,
  execPath: string,
): string {
  return env.PORTABLE_EXECUTABLE_FILE ?? execPath;
}

export function buildWindowsUpdateScript(newExePath: string, exePath: string): string {
  return buildWindowsPortableUpdateScript({ newExePath, exePath });
}

export async function checkForUpdates(serverUrl: string): Promise<void> {
  try {
    const body = await get(`${serverUrl}/api/settings/viewer-version`);
    const { version } = JSON.parse(body) as { version: string };
    const serverVersion = normalizeVersion(version);
    if (!shouldInstallUpdate(serverVersion, app.getVersion(), pendingVersion)) return;
    pendingVersion = serverVersion;

    const tempDir = app.getPath('temp');
    const newExePath = path.join(tempDir, 'CamViewer-new.exe');
    await downloadFile(`${serverUrl}/api/settings/viewer-app`, newExePath);

    // Windows: use a batch script to replace the running EXE after quit.
    // PORTABLE_EXECUTABLE_FILE = 원본 포터블 EXE 경로 (process.execPath는 임시 압축 해제 경로)
    const exePath = resolvePortableExePath(process.env, process.execPath);
    const batPath = path.join(tempDir, 'camviewer-update.bat');
    fs.writeFileSync(
      batPath,
      buildWindowsPortableUpdateScript({ newExePath, exePath }),
      'latin1',
    );

    new Notification({
      title: 'CamViewer 자동 업데이트',
      body: `v${serverVersion} 설치를 위해 CamViewer를 자동 재시작합니다.`,
    }).show();

    exec(`start /b "" "${batPath}"`, { windowsHide: true }, () => {
      app.exit(0);
    });
  } catch {
    // Silently ignore — update check is best-effort
  }
}
