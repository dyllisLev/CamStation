from pydantic import BaseModel, Field
from typing import Any, Literal, Optional

class Camera(BaseModel):
    id: str
    name: str
    online: bool
    has_sub: bool = False
    enabled: bool = True


class CameraConfigStatus(BaseModel):
    id: str
    name: str
    enabled: bool
    online: bool = False
    has_sub: bool = False


class CameraAdminItem(BaseModel):
    id: str
    display_name: str
    location: str | None = None
    enabled: bool
    archived: bool = False
    online: bool = False
    has_sub: bool = False
    main_stream_configured: bool
    sub_stream_configured: bool
    onvif_configured: bool
    sort_order: int = 0
    notes: str | None = None


class CameraCreateRequest(BaseModel):
    id: str = Field(min_length=1, max_length=128)
    display_name: str = Field(min_length=1, max_length=128)
    location: str | None = None
    enabled: bool = True
    main_stream_url: str = Field(min_length=1)
    sub_stream_url: str | None = None
    onvif_host: str | None = None
    onvif_port: int | None = None
    onvif_username: str | None = None
    onvif_password: str | None = None
    sort_order: int = 0
    notes: str | None = None


class CameraUpdateRequest(BaseModel):
    display_name: str | None = None
    location: str | None = None
    enabled: bool | None = None
    main_stream_url: str | None = None
    sub_stream_url: str | None = None
    onvif_host: str | None = None
    onvif_port: int | None = None
    onvif_username: str | None = None
    onvif_password: str | None = None
    sort_order: int | None = None
    notes: str | None = None


class UpdateCameraEnabledRequest(BaseModel):
    enabled: bool


class CameraRebootResult(BaseModel):
    camera_id: str
    endpoint: str
    status: Literal["requested"] = "requested"
    message: str


class MotionEvent(BaseModel):
    camera_id: str
    ts_start: float
    ts_end: Optional[float]

class RecordingSegment(BaseModel):
    camera_id: str
    filename: str
    ts_start: float
    ts_end: float | None = None
    file_size: int | None = None

class Settings(BaseModel):
    retention_days: int
    segment_minutes: int
    motion_threshold: float
    max_storage_gb: int
    motion_enabled: bool = True

class SystemStatus(BaseModel):
    cpu_percent: float
    ram_mb: float
    disk_used_gb: float
    disk_total_gb: float
    cameras_online: int


class LayoutItem(BaseModel):
    i: str
    x: int
    y: int
    w: int
    h: int
    minW: int | None = None
    minH: int | None = None


class LayoutProfile(BaseModel):
    id: str
    name: str
    data: list[LayoutItem]
    timeline_collapsed: bool = False
    grid_cols: int = 12
    grid_rows: int | None = None
    created_at: int
    updated_at: int


class CreateLayoutRequest(BaseModel):
    name: str
    data: list[LayoutItem]
    timeline_collapsed: bool = False
    grid_cols: int = 12
    grid_rows: int | None = None


class UpdateLayoutRequest(BaseModel):
    name: str | None = None
    data: list[LayoutItem] | None = None
    timeline_collapsed: bool | None = None
    grid_cols: int | None = None
    grid_rows: int | None = None


class CameraStorageStats(BaseModel):
    camera_id: str
    total_gb: float
    hourly_gb: float
    oldest_date: Optional[str]
    newest_date: Optional[str]
    days_recorded: int

class StorageStats(BaseModel):
    disk_total_gb: float
    disk_used_gb: float
    disk_free_gb: float
    recordings_gb: float
    cameras: list[CameraStorageStats]
    hourly_gb_total: float


class ViewerCameraState(BaseModel):
    camera_id: str
    connected: bool = False
    video_ready_state: int = 0
    last_binary_at: float | None = None
    last_video_time: float | None = None
    last_video_time_at: float | None = None
    stalled_ms: int = 0
    reconnect_count: int = 0
    error: str | None = None


class ViewerHeartbeat(BaseModel):
    client_id: str = Field(min_length=1, max_length=128)
    name: str = Field(min_length=1, max_length=128)
    app_version: str | None = None
    server_url: str | None = None
    platform: str | None = None
    hostname: str | None = None
    pid: int | None = None
    started_at: float | None = None
    expected_cameras: int = 0
    cameras: list[ViewerCameraState] = Field(default_factory=list)


class ViewerClientStatus(BaseModel):
    client_id: str
    name: str
    app_version: str | None = None
    server_url: str | None = None
    platform: str | None = None
    hostname: str | None = None
    pid: int | None = None
    started_at: float | None = None
    last_seen: float
    expected_cameras: int
    healthy_cameras: int
    state: Literal["healthy", "degraded", "offline", "unknown"]
    cameras: list[ViewerCameraState] = Field(default_factory=list)
    payload: dict[str, Any]


class CreateViewerCommand(BaseModel):
    command: Literal["refresh_streams", "reload_page", "restart_app", "ping"]
    reason: str | None = None


class ViewerCommand(BaseModel):
    id: int
    client_id: str
    command: str
    status: Literal["pending", "claimed", "completed", "failed"]
    reason: str | None = None
    created_at: float
    claimed_at: float | None = None
    completed_at: float | None = None
    result: dict[str, Any] | None = None


class CompleteViewerCommand(BaseModel):
    ok: bool
    message: str | None = None
    details: dict[str, Any] | None = None
