from fastapi import APIRouter, HTTPException
from fastapi.responses import FileResponse
from pathlib import Path
import shutil
import aiosqlite
from datetime import datetime, timezone, timedelta
from config import RECORDINGS_DIR, get_db_path
from models import StorageStats, CameraStorageStats

router = APIRouter(prefix="/api/recordings", tags=["recordings"])

KST = timezone(timedelta(hours=9))


@router.get("/stats", response_model=StorageStats)
async def get_storage_stats():
    base = Path(RECORDINGS_DIR)
    disk = shutil.disk_usage(str(base))
    db_path = get_db_path()

    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute("""
            SELECT
                camera_id,
                COALESCE(SUM(file_size), 0) AS total_bytes,
                MIN(date(ts_start, 'unixepoch', '+9 hours')) AS oldest_date,
                MAX(date(ts_start, 'unixepoch', '+9 hours')) AS newest_date,
                COUNT(DISTINCT date(ts_start, 'unixepoch', '+9 hours')) AS days_recorded
            FROM recordings
            GROUP BY camera_id
        """)
        rows = [dict(r) for r in await cur.fetchall()]

    cam_stats = []
    for row in rows:
        total_gb = row["total_bytes"] / 1024 ** 3
        days = row["days_recorded"]
        hourly_gb = (total_gb / days / 24) if days > 0 else 0.0
        cam_stats.append(CameraStorageStats(
            camera_id=row["camera_id"],
            total_gb=round(total_gb, 2),
            hourly_gb=round(hourly_gb, 3),
            oldest_date=row["oldest_date"],
            newest_date=row["newest_date"],
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
    day_start = datetime.strptime(date, "%Y-%m-%d").replace(tzinfo=KST).timestamp()
    day_end = day_start + 86400
    db_path = get_db_path()
    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute(
            "SELECT camera_id, filename, ts_start, ts_end, file_size FROM recordings "
            "WHERE camera_id=? AND ts_start>=? AND ts_start<? ORDER BY ts_start",
            (cam_id, day_start, day_end),
        )
        return [dict(r) for r in await cur.fetchall()]
