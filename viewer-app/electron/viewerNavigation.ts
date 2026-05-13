export const VIEWER_MODE_QUERY = 'viewer=1';

export function normalizeServerUrl(raw: string): string {
  return raw.trim().replace(/\/+$/, '');
}

export function buildViewerUrl(serverUrl: string): string {
  return `${normalizeServerUrl(serverUrl)}/new?${VIEWER_MODE_QUERY}`;
}

function isRestrictedViewerPath(pathname: string): boolean {
  return (
    pathname === '/viewer'
    || pathname.startsWith('/viewer/')
    || pathname.startsWith('/new/recordings')
    || pathname.startsWith('/new/settings')
  );
}

export function shouldRestrictViewerNavigation(targetUrl: string, serverUrl: string): boolean {
  const normalizedServerUrl = normalizeServerUrl(serverUrl);
  if (!normalizedServerUrl) return false;

  try {
    const target = new URL(targetUrl);
    const server = new URL(normalizedServerUrl);
    return target.origin === server.origin && isRestrictedViewerPath(target.pathname);
  } catch {
    return false;
  }
}
