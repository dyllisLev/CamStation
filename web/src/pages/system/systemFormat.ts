import type { Job } from "../../app/api";

export function formatDate(value?: string): string {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString("ko-KR", { hour12: false });
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function jobBadgeState(state?: string): string {
  switch (state) {
    case "running":
    case "succeeded":
      return "running";
    case "queued":
      return "info";
    case "failed":
      return "error";
    case "cancelled":
    case "deleted":
      return "warning";
    case undefined:
      return "offline";
    default:
      return state;
  }
}

export function isMaintenanceJob(job: Job): boolean {
  return job.kind.startsWith("maintenance");
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "요청 처리 중 오류가 발생했습니다.";
}
