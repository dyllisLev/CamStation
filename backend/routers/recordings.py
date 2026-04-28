from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse
from pathlib import Path
from datetime import datetime
import shutil
from config import RECORDINGS_DIR
from models import StorageStats, CameraStorageStats

router = APIRouter(prefix="/api/recordings", tags=["recordings"])

@router.get("/stats", response_model=StorageStats)
async def get_storage_stats():
    base = Path(RECORDINGS_DIR)
    disk = shutil.disk_usage(str(base))

    cam_stats = []
    for cam_dir in sorted(base.iterdir()):
        if not cam_dir.is_dir():
            continue
        total_bytes = 0
        dates = []
        for day_dir in cam_dir.iterdir():
            if not day_dir.is_dir():
                continue
            try:
                datetime.strptime(day_dir.name, "%Y-%m-%d")
                dates.append(day_dir.name)
            except ValueError:
                continue
            for f in day_dir.glob("*.mp4"):
                total_bytes += f.stat().st_size
        total_gb = total_bytes / 1024 ** 3
        days = len(dates)
        hourly_gb = (total_gb / days / 24) if days > 0 else 0.0
        cam_stats.append(CameraStorageStats(
            camera_id=cam_dir.name,
            total_gb=round(total_gb, 2),
            hourly_gb=round(hourly_gb, 3),
            oldest_date=min(dates) if dates else None,
            newest_date=max(dates) if dates else None,
            days_recorded=days,
        ))

    recordings_gb = sum(c.total_gb for c in cam_stats)
    hourly_total = sum(c.hourly_gb for c in cam_stats)

    return StorageStats(
        disk_total_gb=round(disk.total / 1024 ** 3, 1),
        disk_used_gb=round(disk.used / 1024 ** 3, 1),
        disk_free_gb=round(disk.free / 1024 ** 3, 1),
        recordings_gb=round(recordings_gb, 1),
        cameras=cam_stats,
        hourly_gb_total=round(hourly_total, 3),
    )

@router.get("/{cam_id}/{date}/{filename}")
async def get_recording(cam_id: str, date: str, filename: str):
    path = Path(RECORDINGS_DIR) / cam_id / date / filename
    if not path.exists() or path.suffix != ".mp4":
        raise HTTPException(status_code=404, detail="Recording not found")
    return FileResponse(str(path), media_type="video/mp4")

@router.get("/{cam_id}/{date}")
async def list_recordings(cam_id: str, date: str):
    day_dir = Path(RECORDINGS_DIR) / cam_id / date
    if not day_dir.exists():
        return []
    return sorted([f.name for f in day_dir.glob("*.mp4")])
