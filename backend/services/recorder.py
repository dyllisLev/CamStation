import asyncio
import contextlib
import os
import re
import sqlite3
import aiosqlite
import shutil
from datetime import datetime, timezone, timedelta
from pathlib import Path
import logging
from database import get_setting
from services.recording_health import RecordingHealthIssue, RecordingHealthReport

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
_temp_dir: str = ""
_watchdog_task: asyncio.Task | None = None
_current_segment_paths: dict[str, str] = {}  # cam_id → 현재 세그먼트 temp 파일 전체 경로
_stderr_tasks: dict[str, asyncio.Task] = {}
_maintenance_reason: str | None = None
_event_alert_sender = None


def set_event_alert_sender(sender) -> None:
    global _event_alert_sender
    _event_alert_sender = sender


async def _send_recording_event_alert(
    event: str,
    *,
    camera_id: str,
    message: str,
    path: str | None = None,
    filename: str | None = None,
    file_size: int | None = None,
    severity: str = "ERROR",
) -> None:
    if _event_alert_sender is None:
        return
    issue = RecordingHealthIssue(
        code=event,
        severity=severity,
        camera_id=camera_id,
        message=message,
        path=path,
        filename=filename,
        file_size=file_size,
    )
    report = RecordingHealthReport(
        ok=False,
        checked_at=datetime.now(KST).timestamp(),
        camera_count=len(_processes) or 1,
        active_count=len(get_active()),
        issues=[issue],
    )
    try:
        await _event_alert_sender.send_recording_health_report(report, event=event)
    except Exception as e:
        logger.warning("Recording event alert failed event=%s camera=%s error=%s", event, camera_id, e)


def _next_delay(current: int, ran: float, *, success_threshold: float = 30.0, max_delay: int = 60) -> int:
    if ran >= success_threshold:
        return 5
    return min(max(current * 2, 5), max_delay)


def build_ffmpeg_cmd(source_rtsp: str, output_dir: str, segment_minutes: int) -> list[str]:
    segment_sec = segment_minutes * 60
    output_pattern = os.path.join(output_dir, "%Y-%m-%d_%H-%M.mp4")
    return [
        "ffmpeg", "-y",
        "-nostats",
        "-use_wallclock_as_timestamps", "1",
        "-rtsp_transport", "tcp",
        "-i", source_rtsp,
        "-c:v", "copy",
        "-c:a", "aac",
        "-f", "segment",
        "-segment_time", str(segment_sec),
        "-segment_atclocktime", "1",
        "-reset_timestamps", "1",
        "-strftime", "1",
        "-avoid_negative_ts", "make_zero",
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


async def _execute_db_write_with_retry(
    db_path: str,
    statements: list[tuple[str, tuple]],
    *,
    retries: int = 5,
    base_delay: float = 0.2,
    busy_timeout_ms: int = 5000,
) -> list[int]:
    """Execute DB writes with retry for transient SQLite writer contention."""
    last_error: Exception | None = None
    for attempt in range(retries + 1):
        try:
            async with aiosqlite.connect(db_path, timeout=busy_timeout_ms / 1000) as db:
                await db.execute(f"PRAGMA busy_timeout={busy_timeout_ms}")
                rowcounts: list[int] = []
                for sql, params in statements:
                    result = await db.execute(sql, params)
                    rowcounts.append(result.rowcount)
                await db.commit()
                return rowcounts
        except sqlite3.OperationalError as e:
            last_error = e
            if "database is locked" not in str(e).lower() or attempt >= retries:
                raise
            delay = base_delay * (2 ** attempt)
            logger.warning(
                "DB write locked; retrying attempt=%d/%d delay=%.2fs statements=%d error=%s",
                attempt + 1,
                retries,
                delay,
                len(statements),
                e,
            )
            await asyncio.sleep(delay)
    raise last_error or RuntimeError("DB write failed")


def _date_from_segment_path(path: str) -> str:
    """세그먼트가 속한 날짜를 결정한다.

    ffmpeg 출력 디렉토리는 프로세스 시작 시점의 날짜로 고정될 수 있다. 프로세스가
    자정을 넘겨 계속 실행되면 parent dir은 전날이지만 filename은 현재 날짜가 된다.
    최종 recordings 경로는 filename의 YYYY-MM-DD를 우선 사용해야 한다.
    """
    p = Path(path)
    m = re.match(r"^(\d{4}-\d{2}-\d{2})_\d{2}-\d{2}$", p.stem)
    if m:
        return m.group(1)
    return p.parent.name


async def _terminate_process(proc: asyncio.subprocess.Process, timeout: float = 10.0):
    """Terminate ffmpeg; if it does not exit promptly, kill it."""
    if proc.returncode is not None:
        return
    proc.terminate()
    try:
        await asyncio.wait_for(proc.wait(), timeout=timeout)
    except asyncio.TimeoutError:
        logger.warning("ffmpeg pid %s did not exit after terminate; killing", getattr(proc, "pid", "?"))
        proc.kill()
        with contextlib.suppress(asyncio.TimeoutError):
            await asyncio.wait_for(proc.wait(), timeout=timeout)


async def _move_to_recordings(temp_path: str, cam_id: str, recordings_dir: str) -> bool:
    """temp 경로의 파일을 최종 recordings 경로로 이동"""
    # temp_path: /opt/camstation/temp/cam_id/2026-05-12/2026-05-13_07-30.mp4
    # final:     /opt/camstation/recordings/cam_id/2026-05-13/2026-05-13_07-30.mp4
    temp_p = Path(temp_path)
    date_str = _date_from_segment_path(temp_path)
    final_dir = Path(recordings_dir) / cam_id / date_str
    final_path = final_dir / temp_p.name

    try:
        final_dir.mkdir(parents=True, exist_ok=True)
        if not temp_p.exists() and final_path.exists():
            logger.info(
                "Segment already moved: camera=%s final=%s size=%s",
                cam_id,
                final_path,
                _safe_getsize(str(final_path)),
            )
            return True
        temp_size = _safe_getsize(str(temp_p))
        shutil.move(str(temp_p), str(final_path))
        logger.info(
            "Moved segment: camera=%s temp=%s final=%s size=%s date=%s",
            cam_id,
            temp_path,
            final_path,
            temp_size,
            date_str,
        )
        return True
    except Exception as e:
        logger.exception(
            "Failed to move segment: camera=%s temp=%s final=%s error=%s",
            cam_id,
            temp_path,
            final_path,
            e,
        )
        return False


async def _watch_stderr(cam_id: str, proc: asyncio.subprocess.Process, db_path: str, recordings_dir: str):
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
            logger.info(
                "New recording segment detected: camera=%s pid=%s path=%s filename=%s ts_start=%.3f",
                cam_id,
                getattr(proc, "pid", None),
                new_path,
                filename,
                new_ts,
            )
            if prev_path is not None:
                size = _safe_getsize(prev_path)
                prev_filename = os.path.basename(prev_path)
                logger.info(
                    "Closing previous recording segment: camera=%s prev=%s next=%s size=%s ts_end=%.3f",
                    cam_id,
                    prev_path,
                    new_path,
                    size,
                    new_ts,
                )
                # 이전 세그먼트가 완료됨 → 최종 경로로 이동 후 DB 업데이트
                moved = await _move_to_recordings(prev_path, cam_id, recordings_dir)
                if moved:
                    rowcounts = await _execute_db_write_with_retry(
                        db_path,
                        [(
                            "UPDATE recordings SET ts_end=?, file_size=? "
                            "WHERE camera_id=? AND filename=? AND ts_end IS NULL",
                            (new_ts, size, cam_id, prev_filename),
                        )],
                    )
                    logger.info(
                        "Closed recording DB row: camera=%s filename=%s rowcount=%s size=%s ts_end=%.3f",
                        cam_id,
                        prev_filename,
                        rowcounts[0],
                        size,
                        new_ts,
                    )
                    if rowcounts[0] == 0:
                        # crash/재시작 후 prev segment가 이전 프로세스에서 INSERT된 경우
                        # (ts_end가 이미 채워져 있거나 filename 불일치) → 대안 쿼리
                        logger.warning(
                            "Recording DB close updated 0 rows; trying fallback camera=%s filename=%s ts_end=%.3f size=%s",
                            cam_id, prev_filename, new_ts, size,
                        )
                        fallback_counts = await _execute_db_write_with_retry(
                            db_path,
                            [(
                                "UPDATE recordings SET ts_end=?, file_size=? "
                                "WHERE camera_id=? AND filename=?",
                                (new_ts, size, cam_id, prev_filename),
                            )],
                        )
                        logger.warning(
                            "Recording DB fallback close result: camera=%s filename=%s rowcount=%s",
                            cam_id,
                            prev_filename,
                            fallback_counts[0],
                        )
                else:
                    logger.error(
                        "Previous recording segment move failed; DB close skipped camera=%s prev=%s next=%s",
                        cam_id,
                        prev_path,
                        new_path,
                    )
                    await _send_recording_event_alert(
                        "recording_segment_move_failed",
                        camera_id=cam_id,
                        message=f"이전 녹화 segment 이동 실패로 DB close를 건너뛰었습니다: {prev_path}",
                        path=prev_path,
                        filename=prev_filename,
                        file_size=size,
                    )
            insert_counts = await _execute_db_write_with_retry(
                db_path,
                [(
                    "INSERT OR IGNORE INTO recordings(camera_id, filename, ts_start) VALUES(?,?,?)",
                    (cam_id, filename, new_ts),
                )],
            )
            logger.info(
                "Opened recording DB row: camera=%s filename=%s rowcount=%s ts_start=%.3f",
                cam_id,
                filename,
                insert_counts[0],
                new_ts,
            )
        except Exception as e:
            logger.exception(
                "Recording segment DB handling failed: camera=%s pid=%s new_path=%s prev_path=%s error=%s",
                cam_id,
                getattr(proc, "pid", None),
                new_path,
                prev_path,
                e,
            )
            await _send_recording_event_alert(
                "recording_db_write_failed",
                camera_id=cam_id,
                message=f"녹화 segment DB 처리 실패: {e}",
                path=new_path,
                filename=filename,
            )

        prev_path = new_path
        _current_segment_paths[cam_id] = new_path


async def _run_recording(cam_id: str, segment_minutes: int, recordings_dir: str, temp_dir: str, db_path: str):
    delay = 5
    first_run = True
    while True:
        if cam_id in _stopping_rec:
            break

        # Always read the current segment_minutes from DB (reflects user changes without restart)
        current_segment_min = int(await get_setting("segment_minutes") or "10")

        today = datetime.now(KST).strftime("%Y-%m-%d")
        output_dir = os.path.join(temp_dir, cam_id, today)
        Path(output_dir).mkdir(parents=True, exist_ok=True)

        # 첫 실행 시 이전 실행에서 남은 temp 파일 정리 (완료되지 않은 orphan 제거)
        if first_run:
            first_run = False
            for f in Path(output_dir).glob("*.mp4"):
                try:
                    f.unlink()
                    logger.info("Cleaned up orphan temp file: %s", f)
                except OSError:
                    pass
        source = f"rtsp://127.0.0.1:8554/{cam_id}"
        cmd = build_ffmpeg_cmd(source, output_dir, current_segment_min)
        env = dict(os.environ)
        env["TZ"] = "Asia/Seoul"
        proc = await asyncio.create_subprocess_exec(
            *cmd,
            env=env,
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.PIPE,
        )
        _active_procs[cam_id] = proc
        stderr_task = asyncio.create_task(_watch_stderr(cam_id, proc, db_path, recordings_dir))
        _stderr_tasks[cam_id] = stderr_task
        logger.info("Started recording for %s (pid %s) -> temp:%s", cam_id, proc.pid, output_dir)
        t_start = asyncio.get_running_loop().time()
        await proc.wait()
        ran = asyncio.get_running_loop().time() - t_start

        superseded = _active_procs.get(cam_id) is not proc and cam_id not in _stopping_rec

        # stderr task 정리. 다른 recorder loop가 이미 같은 camera_id를 인수한 경우
        # 새 loop의 stderr task/state를 지우면 안 된다.
        if not stderr_task.done():
            stderr_task.cancel()
            try:
                await stderr_task
            except asyncio.CancelledError:
                pass
        if _stderr_tasks.get(cam_id) is stderr_task:
            _stderr_tasks.pop(cam_id, None)

        if superseded:
            logger.warning(
                "Recording loop for %s exited after being superseded by another process; not retrying",
                cam_id,
            )
            break

        # 마지막 세그먼트: temp → 최종 경로 이동 후 DB 업데이트
        ts_end = datetime.now(KST).timestamp()
        last_path = _current_segment_paths.pop(cam_id, None)
        last_filename = os.path.basename(last_path) if last_path else None
        last_size = None
        if last_path:
            last_size = _safe_getsize(last_path)
            moved = await _move_to_recordings(last_path, cam_id, recordings_dir)
            if not moved:
                await _send_recording_event_alert(
                    "recording_segment_move_failed",
                    camera_id=cam_id,
                    message=f"마지막 녹화 segment 이동 실패: {last_path}",
                    path=last_path,
                    filename=last_filename,
                    file_size=last_size,
                )
        try:
            if last_filename:
                await _execute_db_write_with_retry(
                    db_path,
                    [(
                        "UPDATE recordings SET ts_end=?, file_size=? "
                        "WHERE camera_id=? AND filename=? AND ts_end IS NULL",
                        (ts_end, last_size, cam_id, last_filename),
                    )],
                )
            else:
                await _execute_db_write_with_retry(
                    db_path,
                    [(
                        "UPDATE recordings SET ts_end=?, file_size=? "
                        "WHERE camera_id=? AND ts_end IS NULL",
                        (ts_end, last_size, cam_id),
                    )],
                )
        except Exception as e:
            logger.error("_run_recording DB error for %s: %s", cam_id, e)
            await _send_recording_event_alert(
                "recording_db_write_failed",
                camera_id=cam_id,
                message=f"녹화 종료 segment DB close 실패: {e}",
                path=last_path,
                filename=last_filename,
                file_size=last_size,
            )

        _active_procs.pop(cam_id, None)
        if cam_id in _stopping_rec:
            break

        delay = _next_delay(delay, ran)
        await _send_recording_event_alert(
            "recording_process_failed",
            camera_id=cam_id,
            message=f"ffmpeg 녹화 프로세스가 예기치 않게 종료되었습니다: returncode={proc.returncode} ran={ran:.0f}s retry_in={delay}s",
        )
        logger.info("Recording for %s exited (ran %.0fs), retrying in %ds", cam_id, ran, delay)
        await asyncio.sleep(delay)


async def start_recording(cam_id: str, segment_minutes: int, recordings_dir: str, temp_dir: str):
    if cam_id in _processes:
        return
    from config import get_db_path
    task = asyncio.create_task(
        _run_recording(cam_id, segment_minutes, recordings_dir, temp_dir, get_db_path())
    )
    _processes[cam_id] = task


async def stop_recording(cam_id: str):
    task = _processes.pop(cam_id, None)
    if not task:
        return
    _stopping_rec.add(cam_id)
    proc = _active_procs.get(cam_id)
    if proc and proc.returncode is None:
        await _terminate_process(proc)
    try:
        await asyncio.wait_for(task, timeout=20)
    except asyncio.TimeoutError:
        logger.warning("Recording task for %s did not stop; cancelling", cam_id)
        task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await task
    except Exception:
        pass
    _stopping_rec.discard(cam_id)
    _active_procs.pop(cam_id, None)
    logger.info("Stopped recording for %s", cam_id)


async def _run_sub_keepalive(cam_id: str):
    delay = 5
    source = f"rtsp://127.0.0.1:8554/{cam_id}_sub"
    while True:
        if cam_id in _stopping_sub:
            break
        proc = await asyncio.create_subprocess_exec(
            "ffmpeg", "-y", "-rtsp_transport", "tcp",
            "-i", source,
            "-c", "copy", "-f", "null", "/dev/null",
            stdout=asyncio.subprocess.DEVNULL,
            stderr=asyncio.subprocess.DEVNULL,
        )
        _active_sub_procs[cam_id] = proc
        logger.info("Started sub-stream keepalive for %s (pid %s)", cam_id, proc.pid)
        t_start = asyncio.get_running_loop().time()
        await proc.wait()
        _active_sub_procs.pop(cam_id, None)
        if cam_id in _stopping_sub:
            break
        ran = asyncio.get_running_loop().time() - t_start
        delay = _next_delay(delay, ran)
        logger.info("Sub-stream keepalive for %s exited (ran %.0fs), retrying in %ds", cam_id, ran, delay)
        await asyncio.sleep(delay)


async def start_sub_keepalive(cam_id: str):
    if cam_id in _sub_processes:
        return
    task = asyncio.create_task(_run_sub_keepalive(cam_id))
    _sub_processes[cam_id] = task
    logger.info("Started sub-stream keepalive task for %s", cam_id)


async def stop_sub_keepalive(cam_id: str):
    task = _sub_processes.pop(cam_id, None)
    if not task:
        return
    _stopping_sub.add(cam_id)
    proc = _active_sub_procs.get(cam_id)
    if proc and proc.returncode is None:
        await _terminate_process(proc)
    try:
        await asyncio.wait_for(task, timeout=20)
    except asyncio.TimeoutError:
        logger.warning("Sub-stream keepalive task for %s did not stop; cancelling", cam_id)
        task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await task
    except Exception:
        pass
    _stopping_sub.discard(cam_id)
    _active_sub_procs.pop(cam_id, None)
    logger.info("Stopped sub-stream keepalive for %s", cam_id)


async def _restart_recording_for_new_day(
    cam_id: str,
    *,
    segment_minutes: int,
    recordings_dir: str,
    temp_dir: str,
    stop_timeout: float = 45.0,
):
    """Restart one recorder for KST date rollover, guaranteeing a start attempt.

    At midnight ffmpeg may be busy closing/writing the new segment.  A stuck
    stop must not block the whole watchdog or leave the camera missing from
    `_processes`; health checks then report `recorder_not_active` until a manual
    backend restart.  This helper bounds the stop phase, cleans stale in-memory
    state, and always attempts to start the recorder again.
    """
    try:
        await asyncio.wait_for(stop_recording(cam_id), timeout=stop_timeout)
    except asyncio.TimeoutError:
        logger.warning(
            "Midnight watchdog: stop timed out for %s; forcing recorder restart",
            cam_id,
        )
        await _send_recording_event_alert(
            "recording_rollover_failed",
            camera_id=cam_id,
            message="자정 rollover 중 recorder stop timeout; 강제 재시작을 진행합니다.",
        )
    except Exception as e:
        logger.exception(
            "Midnight watchdog: stop failed for %s; forcing recorder restart",
            cam_id,
        )
        await _send_recording_event_alert(
            "recording_rollover_failed",
            camera_id=cam_id,
            message=f"자정 rollover 중 recorder stop 실패; 강제 재시작을 진행합니다: {e}",
        )
    finally:
        proc = _active_procs.pop(cam_id, None)
        if proc and proc.returncode is None:
            with contextlib.suppress(Exception):
                proc.kill()
        _processes.pop(cam_id, None)
        _stderr_tasks.pop(cam_id, None)
        _current_segment_paths.pop(cam_id, None)
        _stopping_rec.discard(cam_id)

    await start_recording(cam_id, segment_minutes, recordings_dir, temp_dir)


async def _restart_sub_keepalive_for_new_day(cam_id: str, *, stop_timeout: float = 30.0):
    try:
        await asyncio.wait_for(stop_sub_keepalive(cam_id), timeout=stop_timeout)
    except asyncio.TimeoutError:
        logger.warning(
            "Midnight watchdog: sub keepalive stop timed out for %s; forcing restart",
            cam_id,
        )
    except Exception:
        logger.exception(
            "Midnight watchdog: sub keepalive stop failed for %s; forcing restart",
            cam_id,
        )
    finally:
        proc = _active_sub_procs.pop(cam_id, None)
        if proc and proc.returncode is None:
            with contextlib.suppress(Exception):
                proc.kill()
        _sub_processes.pop(cam_id, None)
        _stopping_sub.discard(cam_id)

    await start_sub_keepalive(cam_id)


async def _midnight_watchdog():
    global _maintenance_reason
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
        _maintenance_reason = "midnight_watchdog_restart"
        try:
            # Restart each recorder immediately after stopping it.  This avoids a
            # long all-cameras-down window when closing/moving large 23:30 files
            # takes minutes, and health checks can skip expected transient gaps.
            for cam_id in cam_ids:
                await _restart_recording_for_new_day(
                    cam_id,
                    segment_minutes=_segment_minutes,
                    recordings_dir=_recordings_dir,
                    temp_dir=_temp_dir,
                )
            for cam_id in sub_cam_ids:
                await _restart_sub_keepalive_for_new_day(cam_id)
        finally:
            _maintenance_reason = None


async def start_all(cam_ids: list[str], segment_minutes: int, recordings_dir: str, temp_dir: str):
    global _segment_minutes, _recordings_dir, _temp_dir, _watchdog_task
    _segment_minutes = segment_minutes
    _recordings_dir = recordings_dir
    _temp_dir = temp_dir
    for cam_id in cam_ids:
        await start_recording(cam_id, segment_minutes, recordings_dir, temp_dir)
    _watchdog_task = asyncio.create_task(_midnight_watchdog())


async def stop_all():
    global _watchdog_task
    if _watchdog_task:
        _watchdog_task.cancel()
        _watchdog_task = None

    # Mark every loop as intentionally stopping before awaiting the first one.
    # Otherwise, while shutdown is busy closing a large segment for camera A,
    # camera B can observe its ffmpeg exit and auto-retry, lengthening shutdown
    # and briefly starting new recorders during service restart.
    _stopping_rec.update(_processes.keys())
    _stopping_sub.update(_sub_processes.keys())
    for cam_id in list(_processes.keys()):
        await stop_recording(cam_id)
    for cam_id in list(_sub_processes.keys()):
        await stop_sub_keepalive(cam_id)


def get_active() -> list[str]:
    return list(_processes.keys())


def get_maintenance_reason() -> str | None:
    return _maintenance_reason