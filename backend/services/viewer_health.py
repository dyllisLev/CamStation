import asyncio
import json
import logging
import time
from dataclasses import dataclass, field

import aiosqlite

logger = logging.getLogger(__name__)


@dataclass(slots=True)
class ViewerHealthIssue:
    code: str
    severity: str
    client_id: str
    message: str
    name: str | None = None
    camera_id: str | None = None
    age_sec: float | None = None
    expected_cameras: int | None = None
    healthy_cameras: int | None = None
    app_version: str | None = None
    pid: int | None = None


@dataclass(slots=True)
class ViewerHealthReport:
    ok: bool
    checked_at: float
    client_count: int
    issues: list[ViewerHealthIssue] = field(default_factory=list)


def _issue(
    code: str,
    severity: str,
    client_id: str,
    message: str,
    *,
    name: str | None = None,
    camera_id: str | None = None,
    age_sec: float | None = None,
    expected_cameras: int | None = None,
    healthy_cameras: int | None = None,
    app_version: str | None = None,
    pid: int | None = None,
) -> ViewerHealthIssue:
    return ViewerHealthIssue(
        code=code,
        severity=severity,
        client_id=client_id,
        message=message,
        name=name,
        camera_id=camera_id,
        age_sec=age_sec,
        expected_cameras=expected_cameras,
        healthy_cameras=healthy_cameras,
        app_version=app_version,
        pid=pid,
    )


async def check_viewer_health(
    db_path: str,
    *,
    now_ts: float | None = None,
    max_heartbeat_age_sec: int = 60,
) -> ViewerHealthReport:
    """Check whether Windows viewer EXEs are alive and actually receiving streams.

    The server cannot inspect Windows processes directly. It treats a recent viewer
    heartbeat as EXE liveness, and per-camera heartbeat fields as real stream
    reception status.
    """
    now_ts = now_ts or time.time()
    issues: list[ViewerHealthIssue] = []

    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        rows = await db.execute_fetchall(
            """
            SELECT client_id, name, app_version, pid, last_seen,
                   expected_cameras, healthy_cameras, state, payload_json
            FROM viewer_clients
            ORDER BY last_seen DESC
            """
        )

    if not rows:
        issues.append(_issue(
            "viewer_missing",
            "ERROR",
            "*",
            "등록된 Viewer heartbeat가 없습니다. EXE가 실행되지 않았거나 서버에 한 번도 접속하지 않았습니다.",
        ))
        return ViewerHealthReport(False, now_ts, 0, issues)

    for row in rows:
        age = now_ts - float(row["last_seen"])
        client_id = row["client_id"]
        name = row["name"]
        expected = int(row["expected_cameras"] or 0)
        healthy = int(row["healthy_cameras"] or 0)
        app_version = row["app_version"]
        pid = row["pid"]

        if age > max_heartbeat_age_sec:
            issues.append(_issue(
                "viewer_heartbeat_stale",
                "ERROR",
                client_id,
                f"Viewer heartbeat가 {age:.0f}초 동안 갱신되지 않았습니다. EXE 종료/정지 가능성이 있습니다.",
                name=name,
                age_sec=age,
                expected_cameras=expected,
                healthy_cameras=healthy,
                app_version=app_version,
                pid=pid,
            ))
            continue

        if expected > 0 and healthy < expected:
            issues.append(_issue(
                "viewer_stream_degraded",
                "ERROR",
                client_id,
                f"Viewer가 일부 카메라 영상을 수신하지 못하고 있습니다: healthy={healthy}/{expected}",
                name=name,
                age_sec=age,
                expected_cameras=expected,
                healthy_cameras=healthy,
                app_version=app_version,
                pid=pid,
            ))

        try:
            payload = json.loads(row["payload_json"] or "{}")
        except json.JSONDecodeError:
            payload = {}
        for camera in payload.get("cameras") or []:
            connected = bool(camera.get("connected"))
            ready = int(camera.get("video_ready_state") or 0)
            stalled_ms = int(camera.get("stalled_ms") or 0)
            error = camera.get("error")
            if connected and ready >= 2 and stalled_ms < 30_000 and error is None:
                continue
            camera_id = str(camera.get("camera_id") or "unknown")
            issues.append(_issue(
                "viewer_camera_not_receiving",
                "ERROR",
                client_id,
                "Viewer 카메라 수신 상태가 비정상입니다: "
                f"camera={camera_id} connected={connected} ready={ready} stalled_ms={stalled_ms} error={error}",
                name=name,
                camera_id=camera_id,
                age_sec=age,
                expected_cameras=expected,
                healthy_cameras=healthy,
                app_version=app_version,
                pid=pid,
            ))

    return ViewerHealthReport(
        ok=not any(i.severity == "ERROR" for i in issues),
        checked_at=now_ts,
        client_count=len(rows),
        issues=issues,
    )


def log_viewer_health_report(report: ViewerHealthReport) -> None:
    if report.ok:
        logger.info("viewer_health_ok clients=%d checked_at=%.0f", report.client_count, report.checked_at)
        return
    logger.error(
        "viewer_health_failed clients=%d issue_count=%d checked_at=%.0f",
        report.client_count,
        len(report.issues),
        report.checked_at,
    )
    for issue in report.issues:
        logger.error(
            "viewer_health_issue code=%s severity=%s client=%s name=%s camera=%s age_sec=%s healthy=%s/%s version=%s pid=%s message=%s",
            issue.code,
            issue.severity,
            issue.client_id,
            issue.name,
            issue.camera_id,
            f"{issue.age_sec:.0f}" if issue.age_sec is not None else None,
            issue.healthy_cameras,
            issue.expected_cameras,
            issue.app_version,
            issue.pid,
            issue.message,
        )


async def run_viewer_health_loop(
    db_path: str,
    *,
    interval_sec: int = 300,
    max_heartbeat_age_sec: int = 60,
    alert_sender=None,
) -> None:
    while True:
        try:
            report = await check_viewer_health(
                db_path,
                max_heartbeat_age_sec=max_heartbeat_age_sec,
            )
            log_viewer_health_report(report)
            if alert_sender is not None and not report.ok:
                await alert_sender.send_viewer_health_report(report)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.exception("viewer_health_loop_error error=%s", e)
        await asyncio.sleep(interval_sec)
