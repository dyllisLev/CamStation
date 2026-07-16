import type { BrowserWindowConstructorOptions } from "electron";

export function viewerURL(serverURL: string): string {
  const parsed = new URL(serverURL);
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || !parsed.hostname || parsed.username || parsed.password) {
    throw new Error("Agent supplied an invalid CamStation server URL");
  }
  if ((parsed.pathname !== "/" && parsed.pathname !== "") || parsed.search || parsed.hash) {
    throw new Error("CamStation server URL must not contain a route");
  }
  parsed.pathname = "/live";
  parsed.search = "?viewer=1";
  return parsed.toString();
}

export function browserWindowOptions(preload: string, packaged: boolean): BrowserWindowConstructorOptions {
  return {
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload,
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      devTools: !packaged,
    },
  };
}

export function isNavigationAllowed(candidate: string, liveURL: string): boolean {
  try {
    return new URL(candidate).href === new URL(liveURL).href;
  } catch {
    return false;
  }
}

export function permissionAllowed(_permission: string): boolean {
  return false;
}

export function rendererStateForEvent(event: string): "ready" | "unresponsive" | "failed" | "not_ready" {
  switch (event) {
  case "unresponsive":
    return "unresponsive";
  case "render-process-gone":
    return "failed";
  default:
    return "not_ready";
  }
}
