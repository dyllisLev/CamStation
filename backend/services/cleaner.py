import asyncio
import shutil
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

def _final_path_for_temp(temp_path: Path, temp_dir: Path, recordings_dir: Path) -> Path | None:
    try:
        rel = temp_path.relative_to(temp_dir)
    except ValueError:
        return None
    if len(rel.parts) < 3:
        return None
    cam_id = rel.parts[0]
    date_str = temp_path.parent.name
    stem = temp_path.stem
    if "_" in stem:
        candidate = stem.split("_", 1)[0]
        try:
            datetime.strptime(candidate, "%Y-%m-%d")
            date_str = candidate
        except ValueError:
            pass
    return recordings_dir / cam_id / date_str / temp_path.name

async def quarantine_stale_temp_segments(
    temp_dir: str,
    recordings_dir: str,
    *,
    stale_days: int = 7,
    quarantine_root: str | None = None,
):
    """Move old orphan temp MP4s to quarantine so health checks don't page later.

    Only files older than ``stale_days`` are considered, which keeps current or
    recently failed segments available for normal recording-health recovery.
    """
    temp_base = Path(temp_dir)
    recordings_base = Path(recordings_dir)
    if not temp_base.exists():
        return 0
    cutoff = datetime.now(KST).timestamp() - stale_days * 86400
    quarantine_base = Path(quarantine_root or str(temp_base.parent / "quarantine" / "stale-temp"))
    stamp = datetime.now(KST).strftime("%Y%m%d%H%M%S")
    moved = 0
    for path in sorted(temp_base.glob("**/*.mp4")):
        if not path.is_file():
            continue
        try:
            stat = path.stat()
        except FileNotFoundError:
            continue
        if stat.st_mtime >= cutoff:
            continue
        final_path = _final_path_for_temp(path, temp_base, recordings_base)
        if final_path is not None and final_path.exists() and final_path.stat().st_size < stat.st_size:
            # Keep potentially useful recovery material if the final file is smaller.
            continue
        dest = quarantine_base / stamp / path.relative_to(temp_base)
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.move(str(path), str(dest))
        moved += 1
        logger.info("Quarantined stale temp segment: %s -> %s", path, dest)
    return moved

async def _run_cleanup(recordings_dir: str, get_retention_fn, temp_dir: str | None = None):
    retention = int(await get_retention_fn("retention_days") or "30")
    await delete_expired_segments(recordings_dir, retention)
    max_gb = int(await get_retention_fn("max_storage_gb") or "0")
    await delete_oldest_to_fit(recordings_dir, max_gb)
    if temp_dir:
        await quarantine_stale_temp_segments(temp_dir, recordings_dir)

async def run_cleanup_loop(recordings_dir: str, get_retention_fn, temp_dir: str | None = None):
    global _cleanup_event
    _cleanup_event = asyncio.Event()

    await _run_cleanup(recordings_dir, get_retention_fn, temp_dir)

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

        await _run_cleanup(recordings_dir, get_retention_fn, temp_dir)
