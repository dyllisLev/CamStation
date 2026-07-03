from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import FileResponse, StreamingResponse
from pathlib import Path
import shutil
import asyncio
import logging
import os
import subprocess
import aiosqlite
from datetime import datetime, timezone, timedelta
from config import RECORDINGS_DIR, get_db_path
from models import StorageStats, CameraStorageStats

router = APIRouter(prefix="/api/recordings", tags=["recordings"])

KST = timezone(timedelta(hours=9))
logger = logging.getLogger(__name__)

# rclone on-demand download cache
CACHE_DIR = Path("/mnt/hdd/cache/recordings")
RCLONE_REMOTE = "gdrive:cctv"
_download_locks: dict[str, asyncio.Lock] = {}
_global_lock = asyncio.Lock()


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


async def _get_download_lock(cache_key: str) -> asyncio.Lock:
    """Per-file download lock to prevent duplicate downloads."""
    async with _global_lock:
        if cache_key not in _download_locks:
            _download_locks[cache_key] = asyncio.Lock()
        return _download_locks[cache_key]


def _rclone_download(rel_path: str, dest: Path) -> bool:
    """Download a file from Google Drive via rclone (blocking)."""
    dest.parent.mkdir(parents=True, exist_ok=True)
    remote_src = f"{RCLONE_REMOTE}/{rel_path}"
    result = subprocess.run(
        ["rclone", "copy", remote_src, str(dest.parent),
         "--no-traverse", "--include", dest.name, "--log-level", "WARNING"],
        capture_output=True, text=True, timeout=300,
    )
    if result.returncode != 0:
        logger.error("rclone download failed: %s → %s", remote_src, result.stderr)
        return False
    return dest.exists()


@router.get("/{cam_id}/{date}/{filename}")
async def get_recording(cam_id: str, date: str, filename: str):
    if not filename.endswith(".mp4"):
        raise HTTPException(status_code=400, detail="Invalid filename")

    local_path = Path(RECORDINGS_DIR) / cam_id / date / filename
    if local_path.exists():
        return FileResponse(str(local_path), media_type="video/mp4")

    # Check DB if this file was backed up
    db_path = get_db_path()
    async with aiosqlite.connect(db_path) as db:
        cur = await db.execute(
            "SELECT backed_up FROM recordings WHERE camera_id=? AND date(ts_start,'unixepoch','+9 hours')=? AND filename=?",
            (cam_id, date, filename),
        )
        row = await cur.fetchone()

    if not row:
        raise HTTPException(status_code=404, detail="Recording not found")

    backed_up = row[0]
    if not backed_up:
        # Not backed up and not on disk — truly missing
        raise HTTPException(status_code=404, detail="Recording not found locally and not backed up")

    # backed_up=1 → try rclone download to cache
    cache_key = f"{cam_id}/{date}/{filename}"
    cache_path = CACHE_DIR / cam_id / date / filename

    if cache_path.exists():
        return FileResponse(str(cache_path), media_type="video/mp4")

    lock = await _get_download_lock(cache_key)
    async with lock:
        # Double-check after acquiring lock
        if cache_path.exists():
            return FileResponse(str(cache_path), media_type="video/mp4")

        logger.info("Downloading from Drive: %s", cache_key)
        loop = asyncio.get_event_loop()
        ok = await loop.run_in_executor(None, _rclone_download, cache_key, cache_path)
        if not ok:
            raise HTTPException(status_code=502, detail="Failed to download from backup storage")

        logger.info("Downloaded: %s (%.1f MB)", cache_key, cache_path.stat().st_size / 1024 / 1024)
        return FileResponse(str(cache_path), media_type="video/mp4")


@router.get("/{cam_id}/{date}")
async def list_recordings(cam_id: str, date: str):
    try:
        day_start = datetime.strptime(date, "%Y-%m-%d").replace(tzinfo=KST).timestamp()
    except ValueError:
        raise HTTPException(status_code=400, detail="Invalid date format; expected YYYY-MM-DD")
    day_end = day_start + 86400
    db_path = get_db_path()
    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute(
            "SELECT camera_id, filename, ts_start, ts_end, file_size, backed_up FROM recordings "
            "WHERE camera_id=? AND ts_start>=? AND ts_start<? ORDER BY ts_start",
            (cam_id, day_start, day_end),
        )
        return [dict(r) for r in await cur.fetchall()]
