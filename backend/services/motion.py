import asyncio
import aiosqlite
from datetime import datetime
import logging

logger = logging.getLogger(__name__)

_motion_enabled: bool = True
_running_procs: list = []


def set_motion_enabled(enabled: bool) -> None:
    global _motion_enabled
    _motion_enabled = enabled
    if not enabled:
        for proc in list(_running_procs):
            try:
                proc.terminate()
            except ProcessLookupError:
                pass


async def save_motion_event(cam_id: str, ts_start: float, ts_end: float, db_path: str):
    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            "INSERT INTO motion_events(camera_id, ts_start, ts_end) VALUES(?,?,?)",
            (cam_id, ts_start, ts_end)
        )
        await db.commit()
    logger.info("Motion event saved: %s %.1f-%.1f", cam_id, ts_start, ts_end)


async def monitor_motion(cam_id: str, threshold: float, db_path: str):
    """FFmpeg scene detect — reads from go2rtc internal RTSP."""
    source = f"rtsp://127.0.0.1:8554/{cam_id}"
    cmd = [
        "ffmpeg", "-rtsp_transport", "tcp",
        "-i", source,
        "-vf", f"select='gt(scene,{threshold})',metadata=print:file=-",
        "-an", "-f", "null", "-",
    ]
    while True:
        if not _motion_enabled:
            await asyncio.sleep(10)
            continue
        try:
            proc = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.DEVNULL,
            )
            _running_procs.append(proc)
            motion_start = None
            last_motion = None
            try:
                async for line in proc.stdout:
                    text = line.decode(errors="ignore").strip()
                    if "pts_time" in text:
                        now = datetime.now().timestamp()
                        if motion_start is None:
                            motion_start = now
                        last_motion = now
                    elif motion_start and last_motion:
                        if datetime.now().timestamp() - last_motion > 2:
                            await save_motion_event(cam_id, motion_start, last_motion, db_path)
                            motion_start = None
                            last_motion = None
                await proc.wait()
            finally:
                try:
                    _running_procs.remove(proc)
                except ValueError:
                    pass
        except asyncio.CancelledError:
            if 'proc' in dir() and proc.returncode is None:
                proc.terminate()
                await proc.wait()
            raise
        except Exception as e:
            logger.warning("Motion monitor error for %s: %s — restarting in 10s", cam_id, e)
            await asyncio.sleep(10)
