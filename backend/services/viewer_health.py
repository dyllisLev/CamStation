import asyncio
import contextlib
import json
import logging
import time
from dataclasses import dataclass, field

import aiosqlite

logger = logging.getLogger(__name__)

VIEWER_CAMERA_ACTIVITY_MAX_AGE_SEC = 30


def _viewer_camera_activity_recent(camera: dict, now_ts: float) -> bool:
    for key in ("last_binary_at", "last_video_time_at"):
        value = camera.get(key)
        if value is None:
            continue
        with contextlib.suppress(TypeError, ValueError):
            if now_ts - float(value) <= VIEWER_CAMERA_ACTIVITY_MAX_AGE_SEC:
                return True
    return False


def _viewer_camera_is_receiving(camera: dict, now_ts: float) -> bool:
    connected = bool(camera.get("connected"))
    ready = int(camera.get("video_ready_state") or 0)
    stalled_ms = int(camera.get("stalled_ms") or 0)
    error = camera.get("error")
    if not connected or stalled_ms >= 30_000 or error is not None:
        return False
    # MSE streams can keep receiving fMP4 data while HTMLVideoElement.readyState
    # briefly drops to HAVE_METADATA(1), especially around live-edge/buffer trim.
    # Treat recent binary/video progress as receiving to avoid noisy false alerts.
    return ready >= 2 or _viewer_camera_activity_recent(camera, now_ts)


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
    enabled_camera_ids: list[str] | None = None,
) -> ViewerHealthReport:
    """Check whether Windows viewer EXEs are alive and actually receiving streams.

    The server cannot inspect Windows processes directly. It treats a recent viewer
    heartbeat as EXE liveness, and per-camera heartbeat fields as real stream
    reception status.
    """
    now_ts = now_ts or time.time()
    issues: list[ViewerHealthIssue] = []
    enabled_set = set(enabled_camera_ids) if enabled_camera_ids is not None else None

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

        try:
            payload = json.loads(row["payload_json"] or "{}")
        except json.JSONDecodeError:
            payload = {}
        payload_cameras = payload.get("cameras") or []
        if enabled_set is not None:
            def camera_base_id(camera: dict) -> str:
                camera_id = str(camera.get("camera_id") or "")
                return camera_id[:-4] if camera_id.endswith("_sub") else camera_id

            effective_cameras = [
                camera for camera in payload_cameras
                if camera_base_id(camera) in enabled_set
            ]
            expected = len(enabled_set)
            healthy = sum(
                1 for camera in effective_cameras
                if _viewer_camera_is_receiving(camera, now_ts)
            )
        else:
            effective_cameras = payload_cameras

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

        for camera in effective_cameras:
            connected = bool(camera.get("connected"))
            ready = int(camera.get("video_ready_state") or 0)
            stalled_ms = int(camera.get("stalled_ms") or 0)
            error = camera.get("error")
            if _viewer_camera_is_receiving(camera, now_ts):
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
    get_enabled_camera_ids=None,
) -> None:
    while True:
        try:
            enabled_ids = list(get_enabled_camera_ids()) if get_enabled_camera_ids is not None else None
            report = await check_viewer_health(
                db_path,
                max_heartbeat_age_sec=max_heartbeat_age_sec,
                enabled_camera_ids=enabled_ids,
            )
            log_viewer_health_report(report)
            if alert_sender is not None and not report.ok:
                await alert_sender.send_viewer_health_report(report)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.exception("viewer_health_loop_error error=%s", e)
        await asyncio.sleep(interval_sec)


class ViewerHealthEventNotifier:
    """Debounced event hook for viewer heartbeat/update driven health alerts.

    The periodic loop remains the safety net. This notifier shortens the alert
    path when a heartbeat itself already shows degraded reception, while avoiding
    a webhook storm from frequent heartbeat posts.
    """

    def __init__(
        self,
        db_path: str,
        *,
        alert_sender,
        max_heartbeat_age_sec: int = 60,
        get_enabled_camera_ids=None,
        debounce_sec: float = 30.0,
    ):
        self.db_path = db_path
        self.alert_sender = alert_sender
        self.max_heartbeat_age_sec = max_heartbeat_age_sec
        self.get_enabled_camera_ids = get_enabled_camera_ids
        self.debounce_sec = debounce_sec
        self._tasks: dict[str, asyncio.Task] = {}

    def notify_heartbeat(
        self,
        *,
        client_id: str,
        state: str,
        previous_state: str | None,
        healthy_cameras: int,
        previous_healthy_cameras: int | None,
    ) -> None:
        # Viewer playback can briefly report 0/N or low readyState during MSE
        # buffer/source transitions and app startup. Do not alert immediately on
        # a single degraded heartbeat; schedule one confirmation check and let
        # later healthy heartbeats cancel it. This keeps event-triggered alerts
        # fast for persistent failures while suppressing transient false alarms.
        if state != "degraded":
            if previous_state == "degraded":
                self._cancel(client_id)
            return

        self._schedule(client_id, delay=self.debounce_sec)

    def _cancel(self, client_id: str) -> None:
        existing = self._tasks.pop(client_id, None)
        if existing is not None and not existing.done():
            existing.cancel()

    def _schedule(self, client_id: str, *, delay: float) -> None:
        existing = self._tasks.get(client_id)
        if existing is not None and not existing.done():
            return
        self._tasks[client_id] = asyncio.create_task(self._run(client_id, delay))

    async def _run(self, client_id: str, delay: float) -> None:
        try:
            if delay > 0:
                await asyncio.sleep(delay)
            enabled_ids = (
                list(self.get_enabled_camera_ids())
                if self.get_enabled_camera_ids is not None
                else None
            )
            report = await check_viewer_health(
                self.db_path,
                max_heartbeat_age_sec=self.max_heartbeat_age_sec,
                enabled_camera_ids=enabled_ids,
            )
            log_viewer_health_report(report)
            if not report.ok:
                await self.alert_sender.send_viewer_health_report(report)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.exception("viewer_event_health_check_error client=%s error=%s", client_id, e)
        finally:
            current = asyncio.current_task()
            if self._tasks.get(client_id) is current:
                self._tasks.pop(client_id, None)

    async def close(self) -> None:
        tasks = [task for task in self._tasks.values() if not task.done()]
        self._tasks.clear()
        for task in tasks:
            task.cancel()
        for task in tasks:
            with contextlib.suppress(asyncio.CancelledError):
                await task
