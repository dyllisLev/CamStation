export type Health = {
  ok: boolean;
  mode: string;
  startedAt: string;
};

export type StreamStatus = {
  installed: boolean;
  running: boolean;
  apiUrl: string;
  error?: string;
};

export type Camera = {
  id: number;
  name: string;
  redactedUrl: string;
  streamName: string;
  state: "streaming" | "offline" | "degraded" | "unknown" | string;
  lastProbe?: {
    reachable?: boolean;
    duration?: number;
    format?: string;
    streams?: Array<{
      index: number;
      type: string;
      codec: string;
      width?: number;
      height?: number;
      frameRate?: string;
    }>;
    transportHint?: string;
  };
  createdAt: string;
  updatedAt: string;
};

export type EventLog = {
  id: number;
  createdAt: string;
  source: string;
  level: "info" | "warning" | "error" | string;
  message: string;
  details?: Record<string, unknown>;
};

export type CreateCamera = {
  name: string;
  url: string;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      payload && typeof payload.error === "string"
        ? payload.error
        : `Request failed with ${response.status}`;
    throw new Error(message);
  }
  return payload as T;
}

export const api = {
  health: () => request<Health>("/api/health"),
  cameras: () => request<Camera[]>("/api/cameras"),
  events: () => request<EventLog[]>("/api/events"),
  streamStatus: () => request<StreamStatus>("/api/streams/status"),
  createCamera: (camera: CreateCamera) =>
    request<{ ok: boolean; camera: Camera; go2rtc: StreamStatus; warning?: string }>(
      "/api/cameras",
      {
        method: "POST",
        body: JSON.stringify(camera),
      },
    ),
  restartStreams: () => request<StreamStatus>("/api/streams/restart", { method: "POST" }),
};

