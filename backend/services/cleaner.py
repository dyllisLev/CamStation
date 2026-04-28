import asyncio
from pathlib import Path
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo
import logging

logger = logging.getLogger(__name__)

KST = ZoneInfo("Asia/Seoul")

_cleanup_event: asyncio.Event | None = None

def trigger_cleanup():
    if _cleanup_event is not None:
        _cleanup_event.set()

def _seconds_until_next_hour() -> float:
    now = datetime.now(KST)
    next_hour = (now + timedelta(hours=1)).replace(minute=0, second=0, microsecond=0)
    return (next_hour - now).total_seconds()

async def delete_expired_segments(recordings_dir: str, retention_days: int):
    cutoff = datetime.now(KST) - timedelta(days=retention_days)
    base = Path(recordings_dir)
    if not base.exists():
        return
    for cam_dir in base.iterdir():
        if not cam_dir.is_dir():
            continue
        for day_dir in cam_dir.iterdir():
            if not day_dir.is_dir():
                continue
            try:
                day_dt = datetime.strptime(day_dir.name, "%Y-%m-%d").replace(tzinfo=KST)
            except ValueError:
                continue
            if day_dt < cutoff:
                for f in day_dir.glob("*.mp4"):
                    f.unlink()
                    logger.info("Deleted expired segment: %s", f)
                try:
                    day_dir.rmdir()
                except OSError:
                    pass

async def delete_oldest_to_fit(recordings_dir: str, max_storage_gb: int):
    if max_storage_gb <= 0:
        return
    max_bytes = max_storage_gb * 1024 ** 3
    base = Path(recordings_dir)
    if not base.exists():
        return

    day_dirs = []
    total_bytes = 0
    for cam_dir in base.iterdir():
        if not cam_dir.is_dir():
            continue
        for day_dir in cam_dir.iterdir():
            if not day_dir.is_dir():
                continue
            try:
                day_dt = datetime.strptime(day_dir.name, "%Y-%m-%d").replace(tzinfo=KST)
            except ValueError:
                continue
            dir_size = sum(f.stat().st_size for f in day_dir.glob("*.mp4") if f.is_file())
            total_bytes += dir_size
            day_dirs.append((day_dt, day_dir, dir_size))

    if total_bytes <= max_bytes:
        return

    day_dirs.sort(key=lambda x: x[0])
    for day_dt, day_dir, dir_size in day_dirs:
        if total_bytes <= max_bytes:
            break
        for f in day_dir.glob("*.mp4"):
            f.unlink()
            logger.info("Deleted for storage limit: %s", f)
        try:
            day_dir.rmdir()
        except OSError:
            pass
        total_bytes -= dir_size
        logger.info("Storage cleanup freed ~%.1f MB from %s", dir_size / 1024 ** 2, day_dir)

async def _run_cleanup(recordings_dir: str, get_retention_fn):
    retention = int(await get_retention_fn("retention_days") or "30")
    await delete_expired_segments(recordings_dir, retention)
    max_gb = int(await get_retention_fn("max_storage_gb") or "0")
    await delete_oldest_to_fit(recordings_dir, max_gb)

async def run_cleanup_loop(recordings_dir: str, get_retention_fn):
    global _cleanup_event
    _cleanup_event = asyncio.Event()

    await _run_cleanup(recordings_dir, get_retention_fn)

    while True:
        wait_sec = _seconds_until_next_hour()
        next_kst = (datetime.now(KST) + timedelta(seconds=wait_sec)).replace(minute=0, second=0, microsecond=0)
        logger.info("Next cleanup in %.0f seconds (KST %s)", wait_sec, next_kst.strftime("%H:%M"))
        try:
            await asyncio.wait_for(_cleanup_event.wait(), timeout=wait_sec)
            _cleanup_event.clear()
            logger.info("Cleanup triggered by settings change")
        except asyncio.TimeoutError:
            logger.info("Running scheduled hourly cleanup (KST %s)", datetime.now(KST).strftime("%H:%M"))

        await _run_cleanup(recordings_dir, get_retention_fn)
