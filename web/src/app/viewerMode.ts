export function isViewerMode(search: string): boolean {
  return new URLSearchParams(search).get("viewer") === "1";
}

export function viewerRoute(path: "/live" | "/recordings"): string {
  return `${path}?viewer=1`;
}
