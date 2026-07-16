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
    case "healthy":
    case "ready":
    case "running":
    case "playing":
      return "running";
    case "stale":
    case "control_degraded":
    case "recovering":
    case "restarting":
      return "warning";
    case "offline":
    case "crashed":
    case "failed":
    case "recovery_failed":
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
    case "delivered":
    case "running":
      return "running";
    case "succeeded":
      return "succeeded";
    case "pending":
      return "info";
    case "failed":
    case "rejected":
      return "error";
    case "expired":
    case "cancelled":
    case "deleted":
      return "warning";
    case undefined:
      return "offline";
    default:
      return state;
  }
}

type ViewerHealthAxes = {
  readonly agent?: { readonly state: string };
  readonly control?: { readonly state: string };
};

export function viewerAgentState(viewer: ViewerHealthAxes): string | undefined {
  return viewer.agent?.state;
}

export function viewerControlState(viewer: ViewerHealthAxes): string | undefined {
  return viewer.control?.state;
}

export function canCancelViewerCommand(state?: string): boolean {
  return state === "pending" || state === "delivered";
}

export function displayViewer(viewer: Viewer): string {
  return viewer.label || viewer.displayName || viewer.id;
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "요청 처리 중 오류가 발생했습니다.";
}
