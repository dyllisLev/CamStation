import type { Viewer } from "../../app/api";

export function formatDate(value?: string): string {
  if (!value) {
    return "-";
  }
  return new Date(value).toLocaleString("ko-KR", { hour12: false });
}

export function viewerBadgeState(status?: string): string {
  switch (status) {
    case "online":
    case "active":
      return "running";
    case "stale":
      return "warning";
    case "offline":
      return "offline";
    case undefined:
      return "offline";
    default:
      return status;
  }
}

export function commandBadgeState(state?: string): string {
  switch (state) {
    case "acknowledged":
    case "sent":
      return "running";
    case "pending":
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

export function displayViewer(viewer: Viewer): string {
  return viewer.label || viewer.displayName || viewer.id;
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "요청 처리 중 오류가 발생했습니다.";
}
