export const MOBILE_VIEWER_CAMERAS_PER_PAGE = 4;

const MOBILE_VIEWER_SWIPE_THRESHOLD_PX = 50;

export type MobileViewerSwipeDirection = "left" | "right" | null;

export function mobileViewerPageCount(cameraCount: number) {
  return Math.max(1, Math.ceil(Math.max(0, cameraCount) / MOBILE_VIEWER_CAMERAS_PER_PAGE));
}

export function mobileViewerPageItems<T>(items: readonly T[], page: number) {
  if (!Number.isInteger(page) || page < 0) return [];
  const start = page * MOBILE_VIEWER_CAMERAS_PER_PAGE;
  return items.slice(start, start + MOBILE_VIEWER_CAMERAS_PER_PAGE);
}

export function mobileViewerSwipeDirection(deltaX: number): MobileViewerSwipeDirection {
  if (deltaX < -MOBILE_VIEWER_SWIPE_THRESHOLD_PX) return "left";
  if (deltaX > MOBILE_VIEWER_SWIPE_THRESHOLD_PX) return "right";
  return null;
}

export function clampMobileViewerPage(page: number, totalPages: number) {
  return Math.max(0, Math.min(Math.max(1, totalPages) - 1, Math.trunc(page)));
}

export function mobileViewerPageAfterSwipe(currentPage: number, totalPages: number, deltaX: number) {
  const direction = mobileViewerSwipeDirection(deltaX);
  if (direction === "left") return clampMobileViewerPage(currentPage + 1, totalPages);
  if (direction === "right") return clampMobileViewerPage(currentPage - 1, totalPages);
  return clampMobileViewerPage(currentPage, totalPages);
}
