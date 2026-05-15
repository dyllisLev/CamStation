export interface Camera {
  id: string;
  name: string;
  online: boolean;
  has_sub: boolean;
}

export interface RecordingSegment {
  camera_id: string;
  filename: string;
  ts_start: number;
  ts_end: number | null;
  file_size: number | null;
}

export interface MotionEvent {
  camera_id: string;
  ts_start: number;
  ts_end: number | null;
}

export interface TimelineData {
  segments: RecordingSegment[];
  motion_events: MotionEvent[];
}

export interface Settings {
  retention_days: number;
  segment_minutes: number;
  motion_threshold: number;
  max_storage_gb: number;
  motion_enabled: boolean;
}

export interface SystemStatus {
  cpu_percent: number;
  ram_mb: number;
  disk_used_gb: number;
  disk_total_gb: number;
  cameras_online: number;
}

export interface LayoutItem {
  i: string;
  x: number;
  y: number;
  w: number;
  h: number;
  minW?: number;
  minH?: number;
}

export interface LayoutProfile {
  id: string;
  name: string;
  data: LayoutItem[];
  timeline_collapsed: boolean;
  grid_cols: number;
  grid_rows: number | null;
  created_at: number;
  updated_at: number;
}

export interface CameraStorageStats {
  camera_id: string;
  total_gb: number;
  hourly_gb: number;
  oldest_date: string | null;
  newest_date: string | null;
  days_recorded: number;
}

export interface StorageStats {
  disk_total_gb: number;
  disk_used_gb: number;
  disk_free_gb: number;
  recordings_gb: number;
  cameras: CameraStorageStats[];
  hourly_gb_total: number;
}

export interface SystemVersion {
  current_version: string;
  latest_version: string | null;
  update_available: boolean;
}

export interface ViewerCameraState {
  camera_id: string;
  connected: boolean;
  video_ready_state: number;
  last_binary_at: number | null;
  last_video_time: number | null;
  last_video_time_at: number | null;
  stalled_ms: number;
  reconnect_count: number;
  error: string | null;
}

export interface ViewerHeartbeatPayload {
  client_id: string;
  name: string;
  app_version?: string | null;
  server_url?: string | null;
  platform?: string | null;
  hostname?: string | null;
  pid?: number | null;
  started_at?: number | null;
  expected_cameras: number;
  cameras: ViewerCameraState[];
}

export interface ViewerClientStatus {
  client_id: string;
  name: string;
  app_version: string | null;
  server_url: string | null;
  platform: string | null;
  hostname: string | null;
  pid: number | null;
  started_at: number | null;
  last_seen: number;
  expected_cameras: number;
  healthy_cameras: number;
  state: 'healthy' | 'degraded' | 'offline' | 'unknown';
  payload: ViewerHeartbeatPayload;
}

export interface ViewerCommand {
  id: number;
  client_id: string;
  command: 'refresh_streams' | 'reload_page' | 'restart_app' | 'ping';
  status: 'pending' | 'claimed' | 'completed' | 'failed';
  reason: string | null;
  created_at: number;
  claimed_at: number | null;
  completed_at: number | null;
  result: Record<string, unknown> | null;
}
