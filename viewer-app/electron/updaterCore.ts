export interface WindowsPortableUpdateScriptOptions {
  newExePath: string;
  exePath: string;
}

export function normalizeVersion(version: string | null | undefined): string {
  return (version ?? '').trim();
}

export function shouldInstallUpdate(
  serverVersion: string | null | undefined,
  currentVersion: string,
  pendingVersion: string | null,
): boolean {
  const normalizedServerVersion = normalizeVersion(serverVersion);
  if (!normalizedServerVersion) return false;
  if (normalizedServerVersion === normalizeVersion(currentVersion)) return false;
  if (pendingVersion && normalizedServerVersion === normalizeVersion(pendingVersion)) return false;
  return true;
}

export function buildWindowsPortableUpdateScript({
  newExePath,
  exePath,
}: WindowsPortableUpdateScriptOptions): string {
  return [
    '@echo off',
    'timeout /t 2 /nobreak > nul',
    `move /y "${newExePath}" "${exePath}"`,
    `start "" "${exePath}"`,
    'del "%~f0"',
  ].join('\r\n');
}
