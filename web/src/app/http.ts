import { withAppBase } from "./basePath";

export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | readonly JsonValue[] | { readonly [key: string]: JsonValue };
export type JsonObject = { readonly [key: string]: JsonValue };

type QueryValue = string | number | boolean | readonly string[] | undefined;

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  if (!headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (isCameraManagementMutation(path, init?.method)) {
    headers.set("X-CamStation-Management", "1");
  }
  const response = await fetch(withAppBase(path), {
    ...init,
    headers,
  });
  const payload: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      payload && typeof payload === "object" && "error" in payload && typeof payload.error === "string"
        ? payload.error
        : `Request failed with ${response.status}`;
    throw new Error(message);
  }
  return payload as T;
}

function isCameraManagementMutation(path: string, method = "GET"): boolean {
  const normalizedMethod = method.toUpperCase();
  if (normalizedMethod === "GET" || normalizedMethod === "HEAD") {
    return false;
  }
  return path.startsWith("/api/cameras") || path.startsWith("/api/camera-profiles");
}

export function queryString(params: Readonly<Record<string, QueryValue>>): string {
  const values = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") {
      continue;
    }
    if (Array.isArray(value)) {
      for (const item of value) {
        values.append(key, item);
      }
      continue;
    }
    values.set(key, String(value));
  }
  const encoded = values.toString();
  return encoded ? `?${encoded}` : "";
}
