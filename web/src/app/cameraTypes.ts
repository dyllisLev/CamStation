export type Health = {
  ok: boolean;
  mode: string;
  startedAt: string;
};

export type Camera = {
  id: number;
  name: string;
  redactedUrl: string;
  streamName: string;
  layoutKey?: string;
  recordingStreamName?: string;
  liveStreamName?: string;
  state: "streaming" | "offline" | "degraded" | "unknown" | string;
  manufacturer?: string;
  model?: string;
  profileAdapter?: string;
  host?: string;
  rtspPort?: number;
  httpPort?: number;
  onvifPort?: number;
  channelIndex?: number;
  lastScan?: Record<string, unknown>;
  streams?: CameraStream[];
  lastProbe?: {
    readonly reachable?: boolean;
    readonly duration?: number;
    readonly format?: string;
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

export type CameraStream = {
  id?: number;
  camera_id?: number;
  role: "recording" | "live" | "snapshot" | string;
  label: string;
  source: string;
  redactedUrl?: string;
  go2rtcStreamName: string;
  codec?: string;
  width?: number;
  height?: number;
  fps?: number;
  bitrateKbps?: number;
  profileToken?: string;
  state?: string;
};

export type StreamCandidate = {
  roleHint: "recording" | "live" | "snapshot" | string;
  label: string;
  source: string;
  redactedUrl?: string;
  codec?: string;
  width?: number;
  height?: number;
  fps?: number;
  bitrateKbps?: number;
  profileToken?: string;
};

export type CameraStreamSelection = {
  role: "recording" | "live" | "snapshot" | string;
  profileToken: string;
};

export type DeviceProfile = {
  name?: string;
  host: string;
  manufacturer: string;
  model: string;
  adapter: string;
  rtspPort?: number;
  httpPort?: number;
  onvifPort?: number;
  capabilities: {
    ptz: boolean;
    audio: boolean;
    microphone: boolean;
    speaker: boolean;
    siren: boolean;
    maxPresets?: number;
  };
  channels: Array<{
    index: number;
    label: string;
    candidates: StreamCandidate[];
  }>;
  lastScan?: Record<string, unknown>;
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
    readonly scale: number;
    readonly tx: number;
    readonly ty: number;
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

export type CreateCamera = {
  name: string;
  url?: string;
  streamName?: string;
  host?: string;
  username?: string;
  password?: string;
  rtspPort?: number;
  httpPort?: number;
  onvifPort?: number;
  adapter?: string;
  profile?: DeviceProfile;
  channelIndex?: number;
  streamSelections?: CameraStreamSelection[];
};

export type UpdateCamera = CreateCamera;

export type CameraScanRequest = {
  name?: string;
  url?: string;
  host: string;
  username?: string;
  password?: string;
  rtspPort?: number;
  httpPort?: number;
  onvifPort?: number;
  adapter?: string;
};

export type CameraPreviewRequest = CameraScanRequest & {
  readonly channelIndex?: number;
  readonly profileToken: string;
  readonly role?: "recording" | "live" | "snapshot" | string;
};

export type CameraPreviewResponse = {
  streamName: string;
  expiresAt: string;
};
