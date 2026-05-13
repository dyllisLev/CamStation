from pydantic import BaseModel
from typing import Optional

class Camera(BaseModel):
    id: str
    name: str
    online: bool
    has_sub: bool = False

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
