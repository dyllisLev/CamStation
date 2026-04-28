import asyncio
import os
from datetime import datetime, timezone, timedelta
from pathlib import Path
import logging

logger = logging.getLogger(__name__)

KST = timezone(timedelta(hours=9))

_processes: dict[str, asyncio.subprocess.Process] = {}
_sub_processes: dict[str, asyncio.subprocess.Process] = {}
_segment_minutes: int = 10
_recordings_dir: str = ""
_watchdog_task: asyncio.Task | None = None


def build_ffmpeg_cmd(source_rtsp: str, output_dir: str, segment_minutes: int) -> list[str]:
    segment_sec = segment_minutes * 60
    output_pattern = os.path.join(output_dir, "%Y-%m-%d_%H-%M.mp4")
    return [
        "ffmpeg", "-y",
        "-rtsp_transport", "tcp",
        "-i", source_rtsp,
        "-c:v", "copy",
        "-c:a", "aac",
        "-f", "segment",
        "-segment_time", str(segment_sec),
        "-segment_atclocktime", "1",
        "-reset_timestamps", "1",
        "-strftime", "1",
        output_pattern,
    ]


async def start_recording(cam_id: str, segment_minutes: int, recordings_dir: str):
    if cam_id in _processes:
        return
    today = datetime.now(KST).strftime("%Y-%m-%d")
    output_dir = os.path.join(recordings_dir, cam_id, today)
    Path(output_dir).mkdir(parents=True, exist_ok=True)
    source = f"rtsp://127.0.0.1:8554/{cam_id}"
    cmd = build_ffmpeg_cmd(source, output_dir, segment_minutes)
    env = dict(os.environ)
    env["TZ"] = "Asia/Seoul"
    proc = await asyncio.create_subprocess_exec(
        *cmd,
        env=env,
        stdout=asyncio.subprocess.DEVNULL,
        stderr=asyncio.subprocess.DEVNULL,
    )
    _processes[cam_id] = proc
    logger.info("Started recording for %s (pid %s)", cam_id, proc.pid)


async def stop_recording(cam_id: str):
    proc = _processes.pop(cam_id, None)
    if proc:
        proc.terminate()
        await proc.wait()
        logger.info("Stopped recording for %s", cam_id)


async def start_sub_keepalive(cam_id: str):
    if cam_id in _sub_processes:
        return
    source = f"rtsp://127.0.0.1:8554/{cam_id}_sub"
    proc = await asyncio.create_subprocess_exec(
        "ffmpeg", "-y", "-rtsp_transport", "tcp",
        "-i", source,
        "-c", "copy", "-f", "null", "/dev/null",
        stdout=asyncio.subprocess.DEVNULL,
        stderr=asyncio.subprocess.DEVNULL,
    )
    _sub_processes[cam_id] = proc
    logger.info("Started sub-stream keepalive for %s (pid %s)", cam_id, proc.pid)


async def stop_sub_keepalive(cam_id: str):
    proc = _sub_processes.pop(cam_id, None)
    if proc:
        proc.terminate()
        await proc.wait()


async def _midnight_watchdog():
    """KST 자정에 녹화를 재시작해 날짜 디렉토리를 새로 생성한다."""
    while True:
        now = datetime.now(KST)
        next_midnight = (now + timedelta(days=1)).replace(
            hour=0, minute=0, second=5, microsecond=0
        )
        wait_secs = (next_midnight - now).total_seconds()
        logger.info("Midnight watchdog: sleeping %.0f s until KST %s", wait_secs, next_midnight)
        await asyncio.sleep(wait_secs)

        cam_ids = list(_processes.keys())
        sub_cam_ids = list(_sub_processes.keys())
        logger.info("Midnight watchdog: restarting %d recordings for new KST day", len(cam_ids))
        for cam_id in cam_ids:
            await stop_recording(cam_id)
        for cam_id in sub_cam_ids:
            await stop_sub_keepalive(cam_id)
        await asyncio.sleep(2)
        for cam_id in cam_ids:
            await start_recording(cam_id, _segment_minutes, _recordings_dir)
        for cam_id in sub_cam_ids:
            await start_sub_keepalive(cam_id)


async def start_all(cam_ids: list[str], segment_minutes: int, recordings_dir: str):
    global _segment_minutes, _recordings_dir, _watchdog_task
    _segment_minutes = segment_minutes
    _recordings_dir = recordings_dir
    for cam_id in cam_ids:
        await start_recording(cam_id, segment_minutes, recordings_dir)
    _watchdog_task = asyncio.create_task(_midnight_watchdog())


async def stop_all():
    global _watchdog_task
    if _watchdog_task:
        _watchdog_task.cancel()
        _watchdog_task = None
    for cam_id in list(_processes.keys()):
        await stop_recording(cam_id)
    for cam_id in list(_sub_processes.keys()):
        await stop_sub_keepalive(cam_id)


def get_active() -> list[str]:
    return list(_processes.keys())
