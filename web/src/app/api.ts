import { withAppBase } from "./basePath";

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

export type LayoutItem = {
  i: string;
  x: number;
  y: number;
  w: number;
  h: number;
  minW?: number;
  minH?: number;
  videoZoom?: {
    scale: number;
    tx: number;
    ty: number;
  };
};

export type LayoutProfile = {
  id: string;
  name: string;
  data: LayoutItem[];
  timeline_collapsed: boolean;
  grid_cols: number;
  grid_rows: number | null;
  created_at: number;
  updated_at: number;
};

export type TimelineData = {
  segments: Array<{
    camera_id: string;
    filename: string;
    ts_start: number;
    ts_end: number | null;
    file_size?: number | null;
  }>;
  motion_events: Array<{
    camera_id: string;
    ts_start: number;
    ts_end: number | null;
  }>;
};

export type EventLog = {
  id: number;
  createdAt: string;
  source: string;
  level: "info" | "warning" | "error" | string;
  message: string;
  details?: Record<string, unknown>;
};

export type RecorderStatus = {
  enabled: boolean;
  recordingsDir: string;
  tempDir: string;
  segmentMinutes: number;
  workers: Array<{
    streamName: string;
    camera_id: number;
    state: string;
    input: string;
    current?: string;
    lastError?: string;
  }>;
};

export type RecordingStorage = {
  recordingsDir: string;
  tempDir: string;
  recordingsBytes: number;
  tempBytes: number;
  maxBytes: number;
  autoCleanupEnabled: boolean;
};

export type CleanupResult = {
  maxBytes: number;
  beforeBytes: number;
  afterBytes: number;
  deleted: Array<{
    id: number;
    streamName: string;
    filename: string;
    path: string;
    bytes: number;
  }>;
};

export type CreateCamera = {
  name: string;
  url: string;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(withAppBase(path), {
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
  layouts: () => request<LayoutProfile[]>("/api/layouts"),
  createLayout: (layout: Pick<LayoutProfile, "name" | "data" | "timeline_collapsed" | "grid_cols" | "grid_rows">) =>
    request<LayoutProfile>("/api/layouts", {
      method: "POST",
      body: JSON.stringify(layout),
    }),
  updateLayout: (
    id: string,
    layout: Partial<Pick<LayoutProfile, "name" | "data" | "timeline_collapsed" | "grid_cols" | "grid_rows">>,
  ) =>
    request<LayoutProfile>(`/api/layouts/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(layout),
    }),
  timeline: (camera: string, date: string) =>
    request<TimelineData>(`/api/timeline?cam=${encodeURIComponent(camera)}&date=${encodeURIComponent(date)}`),
  events: () => request<EventLog[]>("/api/events"),
  recorderStatus: () => request<RecorderStatus>("/api/recorders/status"),
  recordingStorage: () => request<RecordingStorage>("/api/recordings/storage"),
  cleanupRecordings: (maxBytes: number) =>
    request<CleanupResult>("/api/recordings/cleanup", {
      method: "POST",
      body: JSON.stringify({ maxBytes }),
    }),
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
