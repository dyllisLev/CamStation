const viewerDownloadRoute = "/api/viewers/app/download";

export function viewerDownloadHref(value: string): typeof viewerDownloadRoute | null {
  return value === viewerDownloadRoute ? viewerDownloadRoute : null;
}

export function formatReleaseSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${Math.round(bytes)} B`;

  const units = ["KB", "MB", "GB", "TB"];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${units[unit]}`;
}
