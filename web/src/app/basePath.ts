const rawBase = import.meta.env.BASE_URL || "/";

export const appBasePath = rawBase.endsWith("/") ? rawBase.slice(0, -1) : rawBase;

export function withAppBase(path: string) {
  if (!appBasePath || appBasePath === "/") return path;
  return `${appBasePath}${path.startsWith("/") ? path : `/${path}`}`;
}
