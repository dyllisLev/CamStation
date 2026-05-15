import asyncio
import inspect
import json
import logging
import os
from dataclasses import dataclass, field
from datetime import datetime, timezone, timedelta
from pathlib import Path

import aiosqlite

logger = logging.getLogger(__name__)
KST = timezone(timedelta(hours=9))


@dataclass(slots=True)
class RecordingHealthIssue:
    code: str
    severity: str
    camera_id: str
    message: str
    path: str | None = None
    filename: str | None = None
    age_sec: float | None = None
    file_size: int | None = None


@dataclass(slots=True)
class RecordingHealthReport:
    ok: bool
    checked_at: float
    camera_count: int
    active_count: int
    issues: list[RecordingHealthIssue] = field(default_factory=list)


@dataclass(slots=True)
class FileProbe:
    format_duration: float | None
    max_stream_duration: float | None


async def probe_mp4_duration(path: str) -> FileProbe | None:
    """Return ffprobe format/stream durations for one MP4 file."""
    proc = await asyncio.create_subprocess_exec(
        "ffprobe",
        "-v", "error",
        "-show_entries", "format=duration:stream=duration",
        "-of", "json",
        path,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    try:
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=10)
    except asyncio.TimeoutError:
        proc.kill()
        await proc.communicate()
        logger.warning("ffprobe timed out path=%s", path)
        return None
    if proc.returncode != 0:
        logger.warning("ffprobe failed path=%s error=%s", path, stderr.decode(errors="replace").strip())
        return None
    try:
        data = json.loads(stdout.decode())
        format_duration = data.get("format", {}).get("duration")
        stream_durations = [
            float(s["duration"])
            for s in data.get("streams", [])
            if s.get("duration") not in (None, "N/A")
        ]
        return FileProbe(
            format_duration=float(format_duration) if format_duration not in (None, "N/A") else None,
            max_stream_duration=max(stream_durations) if stream_durations else None,
        )
    except (ValueError, TypeError, KeyError) as e:
        logger.warning("ffprobe output parse failed path=%s error=%s", path, e)
        return None


async def _call_probe(probe_func, path: str) -> FileProbe | None:
    result = probe_func(path)
    if inspect.isawaitable(result):
        return await result
    return result


def _date_from_filename_or_ts(filename: str, ts_start: float) -> str:
    stem = Path(filename).stem
    if "_" in stem:
        date_part = stem.split("_", 1)[0]
        try:
            datetime.strptime(date_part, "%Y-%m-%d")
            return date_part
        except ValueError:
            pass
    return datetime.fromtimestamp(ts_start, tz=KST).strftime("%Y-%m-%d")


def _latest_mp4(path: Path) -> Path | None:
    files = [p for p in path.glob("**/*.mp4") if p.is_file()]
    if not files:
        return None
    return max(files, key=lambda p: p.stat().st_mtime)


def _issue(
    code: str,
    severity: str,
    camera_id: str,
    message: str,
    *,
    path: str | None = None,
    filename: str | None = None,
    age_sec: float | None = None,
    file_size: int | None = None,
) -> RecordingHealthIssue:
    return RecordingHealthIssue(
        code=code,
        severity=severity,
        camera_id=camera_id,
        message=message,
        path=path,
        filename=filename,
        age_sec=age_sec,
        file_size=file_size,
    )


async def check_recording_health(
    cam_ids: list[str],
    recordings_dir: str,
    temp_dir: str,
    db_path: str,
    *,
    segment_minutes: int,
    active_cam_ids: list[str],
    now_ts: float | None = None,
    stale_factor: float = 2.0,
    probe_func=probe_mp4_duration,
) -> RecordingHealthReport:
    """Check whether continuous recording is actually producing and moving segments.

    The recorder task being active is not enough: this also checks temp freshness,
    stale temp files that were not moved, DB open rows, and recent DB rows whose
    final MP4 is missing or empty.
    """
    now_ts = now_ts or datetime.now(KST).timestamp()
    segment_sec = max(segment_minutes, 1) * 60
    stale_after = segment_sec * stale_factor
    active = set(active_cam_ids)
    monitored = set(cam_ids)
    issues: list[RecordingHealthIssue] = []
    base_recordings = Path(recordings_dir)
    base_temp = Path(temp_dir)

    for cam_id in cam_ids:
        if cam_id not in active:
            issues.append(_issue(
                "recorder_not_active",
                "ERROR",
                cam_id,
                f"녹화 프로세스가 활성 목록에 없습니다: camera={cam_id}",
            ))

        cam_temp_dir = base_temp / cam_id
        latest_temp = _latest_mp4(cam_temp_dir) if cam_temp_dir.exists() else None
        if latest_temp:
            age = now_ts - latest_temp.stat().st_mtime
            if age > stale_after:
                issues.append(_issue(
                    "segment_not_moved",
                    "ERROR",
                    cam_id,
                    f"temp 세그먼트가 {age:.0f}초 동안 recordings로 이동되지 않았습니다: {latest_temp}",
                    path=str(latest_temp),
                    filename=latest_temp.name,
                    age_sec=age,
                    file_size=latest_temp.stat().st_size,
                ))
        else:
            latest_done = _latest_mp4(base_recordings / cam_id) if (base_recordings / cam_id).exists() else None
            if latest_done is None or now_ts - latest_done.stat().st_mtime > stale_after:
                issues.append(_issue(
                    "no_recent_segment_file",
                    "WARNING",
                    cam_id,
                    f"최근 temp/recordings MP4 파일을 찾지 못했습니다: camera={cam_id}",
                ))

    if os.path.exists(db_path):
        async with aiosqlite.connect(db_path) as db:
            db.row_factory = aiosqlite.Row
            open_rows = await db.execute_fetchall(
                """
                SELECT camera_id, filename, ts_start, file_size
                FROM recordings
                WHERE ts_end IS NULL
                """
            )
            for row in open_rows:
                if row["camera_id"] not in monitored:
                    continue
                age = now_ts - float(row["ts_start"])
                if age > stale_after:
                    issues.append(_issue(
                        "db_open_segment_stale",
                        "ERROR",
                        row["camera_id"],
                        f"DB에 열린 녹화 레코드가 {age:.0f}초 동안 닫히지 않았습니다: {row['filename']}",
                        filename=row["filename"],
                        age_sec=age,
                        file_size=row["file_size"],
                    ))

            cutoff = now_ts - max(segment_sec * 4, 3600)
            recent_rows = await db.execute_fetchall(
                """
                SELECT camera_id, filename, ts_start, ts_end, file_size
                FROM recordings
                WHERE ts_end IS NOT NULL AND ts_end >= ?
                ORDER BY ts_end DESC
                """,
                (cutoff,),
            )
            for row in recent_rows:
                if row["camera_id"] not in monitored:
                    continue
                date_str = _date_from_filename_or_ts(row["filename"], float(row["ts_start"]))
                final_path = base_recordings / row["camera_id"] / date_str / row["filename"]
                if not final_path.exists():
                    issues.append(_issue(
                        "recording_file_missing",
                        "ERROR",
                        row["camera_id"],
                        f"DB에는 완료된 녹화가 있으나 파일이 없습니다: {final_path}",
                        path=str(final_path),
                        filename=row["filename"],
                        file_size=row["file_size"],
                    ))
                elif final_path.stat().st_size <= 0:
                    issues.append(_issue(
                        "recording_file_empty",
                        "ERROR",
                        row["camera_id"],
                        f"완료된 녹화 파일 크기가 0입니다: {final_path}",
                        path=str(final_path),
                        filename=row["filename"],
                        file_size=final_path.stat().st_size,
                    ))
                elif probe_func is not None:
                    probe = await _call_probe(probe_func, str(final_path))
                    if (
                        probe
                        and probe.format_duration is not None
                        and probe.max_stream_duration is not None
                        and probe.max_stream_duration > 0
                        and probe.format_duration - probe.max_stream_duration > 60
                        and probe.format_duration / probe.max_stream_duration > 1.5
                    ):
                        issues.append(_issue(
                            "recording_duration_mismatch",
                            "ERROR",
                            row["camera_id"],
                            "MP4 컨테이너 duration과 실제 stream duration 차이가 큽니다: "
                            f"format={probe.format_duration:.1f}s stream={probe.max_stream_duration:.1f}s path={final_path}",
                            path=str(final_path),
                            filename=row["filename"],
                            file_size=final_path.stat().st_size,
                        ))

    return RecordingHealthReport(
        ok=not any(i.severity == "ERROR" for i in issues),
        checked_at=now_ts,
        camera_count=len(cam_ids),
        active_count=len(active),
        issues=issues,
    )


def log_recording_health_report(report: RecordingHealthReport) -> None:
    if report.ok:
        logger.info(
            "recording_health_ok cameras=%d active=%d checked_at=%.0f",
            report.camera_count,
            report.active_count,
            report.checked_at,
        )
        return

    logger.error(
        "recording_health_failed cameras=%d active=%d issue_count=%d checked_at=%.0f",
        report.camera_count,
        report.active_count,
        len(report.issues),
        report.checked_at,
    )
    for issue in report.issues:
        log = logger.error if issue.severity == "ERROR" else logger.warning
        log(
            "recording_health_issue code=%s severity=%s camera=%s filename=%s path=%s age_sec=%s file_size=%s message=%s",
            issue.code,
            issue.severity,
            issue.camera_id,
            issue.filename,
            issue.path,
            f"{issue.age_sec:.0f}" if issue.age_sec is not None else None,
            issue.file_size,
            issue.message,
        )


async def run_recording_health_loop(
    cam_ids: list[str],
    recordings_dir: str,
    temp_dir: str,
    db_path: str,
    *,
    get_active_cam_ids,
    get_segment_minutes,
    get_camera_ids=None,
    interval_sec: int = 300,
    alert_sender=None,
) -> None:
    while True:
        try:
            segment_minutes = int(await get_segment_minutes())
            current_cam_ids = list(get_camera_ids()) if get_camera_ids is not None else cam_ids
            report = await check_recording_health(
                current_cam_ids,
                recordings_dir,
                temp_dir,
                db_path,
                segment_minutes=segment_minutes,
                active_cam_ids=list(get_active_cam_ids()),
            )
            log_recording_health_report(report)
            if alert_sender is not None and not report.ok:
                await alert_sender.send_recording_health_report(report)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.exception("recording_health_loop_error error=%s", e)
        await asyncio.sleep(interval_sec)
