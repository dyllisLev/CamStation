import { app, Notification } from 'electron';
import http from 'http';
import https from 'https';
import fs from 'fs';
import path from 'path';
import { exec } from 'child_process';

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

export async function checkForUpdates(serverUrl: string): Promise<void> {
  try {
    const body = await get(`${serverUrl}/api/settings/viewer-version`);
    const { version: serverVersion } = JSON.parse(body) as { version: string };
    if (serverVersion === app.getVersion()) return;
    if (serverVersion === pendingVersion) return;
    pendingVersion = serverVersion;

    const tempDir = app.getPath('temp');
    const newExePath = path.join(tempDir, 'CamViewer-new.exe');
    await downloadFile(`${serverUrl}/api/settings/viewer-app`, newExePath);

    // Windows: use a batch script to replace the running EXE after quit
    // PORTABLE_EXECUTABLE_FILE = 원본 포터블 EXE 경로 (process.execPath는 임시 압축 해제 경로)
    const exePath = process.env.PORTABLE_EXECUTABLE_FILE ?? process.execPath;
    const batPath = path.join(tempDir, 'camviewer-update.bat');
    const bat = [
      '@echo off',
      'timeout /t 2 /nobreak > nul',
      `move /y "${newExePath}" "${exePath}"`,
      `start "" "${exePath}"`,
      'del "%~f0"',
    ].join('\r\n');
    fs.writeFileSync(batPath, bat, 'latin1');

    new Notification({
      title: 'CamViewer 업데이트 준비',
      body: `v${serverVersion}으로 업데이트됩니다. 재시작하면 적용됩니다.`,
    }).show();

    app.once('before-quit', () => {
      exec(`start /b "" "${batPath}"`, { windowsHide: true });
    });
  } catch {
    // Silently ignore — update check is best-effort
  }
}
