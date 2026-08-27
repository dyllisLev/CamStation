import type { PlaybackSurface } from "./playbackDiagnosticsApi";

export function isViewerMode(search: string): boolean {
  return new URLSearchParams(search).get("viewer") === "1";
}

export function livePlaybackSurface(search: string, hasNativeBridge: boolean): PlaybackSurface {
  if (!isViewerMode(search)) return "operator_live";
  return hasNativeBridge ? "official_viewer" : "viewer_browser";
}

export function viewerRoute(path: "/live" | "/recordings"): string {
  return `${path}?viewer=1`;
}
