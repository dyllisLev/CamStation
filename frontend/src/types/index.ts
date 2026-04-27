export interface Camera {
  id: string;
  name: string;
  online: boolean;
}

export interface RecordingSegment {
  camera_id: string;
  date: string;
  filename: string;
  ts_start: number;
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
  created_at: number;
  updated_at: number;
}
