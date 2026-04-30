import asyncio
import os
import re
import aiosqlite
from datetime import datetime, timezone, timedelta
from pathlib import Path
import logging

logger = logging.getLogger(__name__)

KST = timezone(timedelta(hours=9))

# Task handles (while True 루프를 담은 asyncio.Task)
_processes: dict[str, asyncio.Task] = {}
_sub_processes: dict[str, asyncio.Task] = {}

# 현재 실행 중인 ffmpeg Process (stop 시 terminate용)
_active_procs: dict[str, asyncio.subprocess.Process] = {}
_active_sub_procs: dict[str, asyncio.subprocess.Process] = {}

# 정상 종료 신호 (루프가 종료 후 재시작하지 않도록)
_stopping_rec: set[str] = set()
_stopping_sub: set[str] = set()
_segment_minutes: int = 10
_recordings_dir: str = ""
_watchdog_task: asyncio.Task | None = None
_current_segment_paths: dict[str, str] = {}  # cam_id → 현재 세그먼트 파일 전체 경로
_stderr_tasks: dict[str, asyncio.Task] = {}


def _next_delay(current: int, ran: float, *, success_threshold: float = 30.0, max_delay: int = 60) -> int:
    if ran >= success_threshold:
        return 5
    return min(max(current * 2, 5), max_delay)


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


def parse_stderr_line(line: str) -> str | None:
    m = re.search(r"Opening '(.+?\.mp4)' for writing", line)
    return m.group(1) if m else None


def _ts_from_path(path: str) -> float | None:
    try:
        stem = Path(path).stem  # "2026-04-28_14-30" or "14-30"
        if "_" in stem:
            # YYYY-MM-DD_HH-MM 형식
            date_part, time_part = stem.split("_", 1)
            hh, mm = time_part.split("-")
            dt = datetime.strptime(f"{date_part} {hh}:{mm}", "%Y-%m-%d %H:%M").replace(tzinfo=KST)
        else:
            # HH-MM 형식 (구형 파일 호환)
            date_str = Path(path).parent.name
            hh, mm = stem.split("-")
            dt = datetime.strptime(f"{date_str} {hh}:{mm}", "%Y-%m-%d %H:%M").replace(tzinfo=KST)
        return dt.timestamp()
    except (ValueError, IndexError):
        return None


def _safe_getsize(path: str) -> int | None:
    try:
        return os.path.getsize(path)
    except OSError:
        return None


async def _watch_stderr(cam_id: str, proc: asyncio.subprocess.Process, db_path: str):
    prev_path: str | None = None

    async for raw in proc.stderr:
        line = raw.decode(errors="replace").strip()
        new_path = parse_stderr_line(line)
        if new_path is None:
            continue

        new_ts = _ts_from_path(new_path)
        if new_ts is None:
            continue

        filename = os.path.basename(new_path)

        try:
            async with aiosqlite.connect(db_path) as db:
                if prev_path is not None:
                    size = _safe_getsize(prev_path)
                    prev_filename = os.path.basename(prev_path)
                    await db.execute(
                        "UPDATE recordings SET ts_end=?, file_size=? "
                        "WHERE camera_id=? AND filename=? AND ts_end IS NULL",
                        (new_ts, size, cam_id, prev_filename),
                    )
                await db.execute(
                    "INSERT OR IGNORE INTO recordings(camera_id, filename, ts_start) VALUES(?,?,?)",
                    (cam_id, filename, new_ts),
                )
                await db.commit()
        except Exception as e:
            logger.error("_watch_stderr DB error for %s: %s", cam_id, e)

        prev_path = new_path
        _current_segment_paths[cam_id] = new_path


async def start_recording(cam_id: str, segment_minutes: int, recordings_dir: str):
    if cam_id in _processes:
        return
    from config import get_db_path
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
        stderr=asyncio.subprocess.PIPE,
    )
    _processes[cam_id] = proc
    _stderr_tasks[cam_id] = asyncio.create_task(_watch_stderr(cam_id, proc, get_db_path()))
    logger.info("Started recording for %s (pid %s)", cam_id, proc.pid)


async def stop_recording(cam_id: str):
    from config import get_db_path
    proc = _processes.pop(cam_id, None)
    if proc:
        proc.terminate()
        await proc.wait()
        task = _stderr_tasks.pop(cam_id, None)
        if task and not task.done():
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass
        # 마지막 세그먼트 완료 처리
        ts_end = datetime.now(KST).timestamp()
        last_path = _current_segment_paths.pop(cam_id, None)
        size = _safe_getsize(last_path) if last_path else None
        last_filename = os.path.basename(last_path) if last_path else None
        try:
            async with aiosqlite.connect(get_db_path()) as db:
                if last_filename:
                    await db.execute(
                        "UPDATE recordings SET ts_end=?, file_size=? "
                        "WHERE camera_id=? AND filename=? AND ts_end IS NULL",
                        (ts_end, size, cam_id, last_filename),
                    )
                else:
                    await db.execute(
                        "UPDATE recordings SET ts_end=?, file_size=? "
                        "WHERE camera_id=? AND ts_end IS NULL",
                        (ts_end, size, cam_id),
                    )
                await db.commit()
        except Exception as e:
            logger.error("stop_recording DB error for %s: %s", cam_id, e)
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
