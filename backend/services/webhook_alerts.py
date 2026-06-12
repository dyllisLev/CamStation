import hashlib
import hmac
import json
import logging
import os
import time
from dataclasses import asdict
from typing import Awaitable, Callable

import httpx

from services.recording_health import RecordingHealthReport
from services.viewer_health import ViewerHealthReport

logger = logging.getLogger(__name__)

_health_alert_suppressed_until = 0.0
_health_alert_suppression_reason = ""

_CAMERA_SOURCE_ISSUE_CODES = {
    "recording_process_failed",
    "viewer_camera_not_receiving",
    "viewer_stream_degraded",
}
_CAMERA_SOURCE_RECOVERY_EVENT = "camera_source_recovered"


def suppress_health_alerts_for_camera_apply(*, seconds: int, reason: str = "camera registry apply") -> None:
    """Temporarily suppress health webhooks during camera config runtime transitions.

    go2rtc restarts, recorder reconciliation, and Viewer reloads can briefly make the
    health checker observe 0/N or partial Viewer/recorder state. Suppress webhook
    delivery for that known transition window; the health checks still run and log.
    """
    global _health_alert_suppressed_until, _health_alert_suppression_reason
    if seconds <= 0:
        return
    now = time.time()
    _health_alert_suppressed_until = max(_health_alert_suppressed_until, now + seconds)
    _health_alert_suppression_reason = reason
    logger.info(
        "Webhook health alerts suppressed for camera apply reason=%s until=%.0f seconds=%d",
        reason,
        _health_alert_suppressed_until,
        seconds,
    )


def _base_camera_id(camera_id: str | None) -> str | None:
    if not camera_id:
        return None
    if camera_id.endswith("_sub"):
        return camera_id[:-4]
    return camera_id


class WebhookAlertSender:
    def __init__(
        self,
        *,
        url: str,
        secret: str,
        cooldown_sec: int = 300,
        post_func: Callable[..., Awaitable[object]] | None = None,
        time_func: Callable[[], float] | None = None,
        camera_incident_summary_sec: int | None = None,
    ):
        self.url = url
        self.secret = secret
        self.cooldown_sec = cooldown_sec
        self.post_func = post_func or self._httpx_post
        self.time_func = time_func or time.time
        self.camera_incident_summary_sec = camera_incident_summary_sec or int(
            os.environ.get("CAMSTATION_CAMERA_INCIDENT_SUMMARY_SEC", "3600")
        )
        self._last_sent_at: dict[str, float] = {}
        self._camera_incidents: dict[str, dict] = {}

    async def _httpx_post(self, url: str, *, content: bytes, headers: dict[str, str], timeout: int):
        async with httpx.AsyncClient(timeout=timeout) as client:
            return await client.post(url, content=content, headers=headers)

    def _dedup_key(self, report: RecordingHealthReport | ViewerHealthReport) -> str:
        parts = []
        for i in report.issues:
            camera = getattr(i, "camera_id", None) or ""
            filename = getattr(i, "filename", None) or ""
            path = getattr(i, "path", None) or ""
            client = getattr(i, "client_id", None) or ""
            healthy = getattr(i, "healthy_cameras", None)
            expected = getattr(i, "expected_cameras", None)
            parts.append(f"{i.severity}:{i.code}:{client}:{camera}:{filename}:{path}:{healthy}:{expected}")
        return "|".join(sorted(parts))

    def _headers(self, body: bytes) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self.secret:
            headers["X-Webhook-Signature"] = hmac.new(
                self.secret.encode(), body, hashlib.sha256
            ).hexdigest()
        return headers

    def _payload(self, report: RecordingHealthReport, *, event: str = "recording_health_failed") -> dict:
        severities = {i.severity for i in report.issues}
        severity = "ERROR" if "ERROR" in severities else "WARNING"
        return {
            "service": "camstation-backend",
            "event": event,
            "severity": severity,
            "message": f"CamStation recording event failed: {len(report.issues)} issue(s)" if event != "recording_health_failed" else f"CamStation recording health check failed: {len(report.issues)} issue(s)",
            "checked_at": report.checked_at,
            "camera_count": report.camera_count,
            "active_count": report.active_count,
            "issues": [asdict(i) for i in report.issues],
        }

    def _viewer_payload(self, report: ViewerHealthReport) -> dict:
        severities = {i.severity for i in report.issues}
        severity = "ERROR" if "ERROR" in severities else "WARNING"
        return {
            "service": "camstation-backend",
            "event": "viewer_health_failed",
            "severity": severity,
            "message": f"CamStation viewer health check failed: {len(report.issues)} issue(s)",
            "checked_at": report.checked_at,
            "client_count": report.client_count,
            "issues": [asdict(i) for i in report.issues],
        }

    def _camera_incident_payload(self, camera_id: str, incident: dict, now: float) -> dict:
        elapsed = max(0, int(now - incident["started_at"]))
        return {
            "camera_id": camera_id,
            "root_cause": "camera_source_or_network_unreachable",
            "started_at": incident["started_at"],
            "elapsed_sec": elapsed,
            "events": sorted(incident["events"]),
            "domains": sorted(incident["domains"]),
        }

    def _classify_camera_source_incident(
        self,
        report: RecordingHealthReport | ViewerHealthReport,
        *,
        event: str,
        domain: str,
    ) -> str | None:
        if not report.issues:
            return None
        if event not in {"recording_process_failed", "viewer_health_failed"}:
            return None

        camera_ids: set[str] = set()
        saw_source_code = False
        for issue in report.issues:
            if issue.code not in _CAMERA_SOURCE_ISSUE_CODES:
                return None
            saw_source_code = True
            base = _base_camera_id(getattr(issue, "camera_id", None))
            if base:
                camera_ids.add(base)

        if not saw_source_code or len(camera_ids) != 1:
            return None
        return next(iter(camera_ids))

    def _register_camera_incident(
        self,
        camera_id: str,
        report: RecordingHealthReport | ViewerHealthReport,
        *,
        event: str,
        domain: str,
        now: float,
    ) -> tuple[dict, bool]:
        incident = self._camera_incidents.get(camera_id)
        is_new = incident is None
        if incident is None:
            incident = {
                "started_at": getattr(report, "checked_at", now) or now,
                "last_sent_at": None,
                "events": set(),
                "domains": set(),
                "recovered_domains": set(),
            }
            self._camera_incidents[camera_id] = incident
        incident["events"].add(event)
        incident["domains"].add(domain)
        incident["recovered_domains"].discard(domain)
        for issue in report.issues:
            incident["events"].add(issue.code)
        return incident, is_new

    async def _post_payload(self, payload: dict, *, key: str | None = None, now: float | None = None) -> bool:
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
        try:
            response = await self.post_func(
                self.url,
                content=body,
                headers=self._headers(body),
                timeout=5,
            )
            if hasattr(response, "raise_for_status"):
                response.raise_for_status()
            if key is not None:
                self._last_sent_at[key] = self.time_func() if now is None else now
            logger.info("Webhook alert delivered event=%s issue_count=%d", payload["event"], len(payload.get("issues", [])))
            return True
        except Exception as e:
            logger.warning("Webhook alert delivery failed url=%s error=%s", self.url, e)
            return False

    def _apply_camera_incident_throttle(
        self,
        payload: dict,
        report: RecordingHealthReport | ViewerHealthReport,
        *,
        event: str,
        domain: str,
        now: float,
    ) -> bool:
        camera_id = self._classify_camera_source_incident(report, event=event, domain=domain)
        if camera_id is None:
            return False
        incident, is_new = self._register_camera_incident(camera_id, report, event=event, domain=domain, now=now)
        last_sent = incident.get("last_sent_at")
        if last_sent is not None and now - last_sent < self.camera_incident_summary_sec:
            logger.info(
                "Webhook alert suppressed by camera incident camera=%s event=%s elapsed_sec=%.0f next_summary_in=%.0f",
                camera_id,
                event,
                now - incident["started_at"],
                self.camera_incident_summary_sec - (now - last_sent),
            )
            return True
        payload["incident"] = self._camera_incident_payload(camera_id, incident, now)
        if is_new:
            payload["message"] = f"CamStation camera source incident: {camera_id} source/network appears unreachable"
        else:
            payload["message"] = f"CamStation camera source incident still active: {camera_id}"
        incident["last_sent_at"] = now
        return False

    async def _maybe_send_camera_recovery(self, *, domain: str, now: float) -> bool:
        if not self.url:
            return False
        delivered = False
        for camera_id, incident in list(self._camera_incidents.items()):
            if domain not in incident["domains"]:
                continue
            incident["recovered_domains"].add(domain)
            if not incident["domains"].issubset(incident["recovered_domains"]):
                continue
            payload = {
                "service": "camstation-backend",
                "event": _CAMERA_SOURCE_RECOVERY_EVENT,
                "severity": "INFO",
                "message": f"CamStation camera source recovered: {camera_id}",
                "checked_at": now,
                "incident": self._camera_incident_payload(camera_id, incident, now),
                "issues": [],
            }
            if await self._post_payload(payload):
                delivered = True
                self._camera_incidents.pop(camera_id, None)
        return delivered

    async def observe_recording_health_report(self, report: RecordingHealthReport) -> bool:
        if report.ok:
            return await self._maybe_send_camera_recovery(domain="recording", now=self.time_func())
        return await self.send_recording_health_report(report)

    async def observe_viewer_health_report(self, report: ViewerHealthReport) -> bool:
        if report.ok:
            return await self._maybe_send_camera_recovery(domain="viewer", now=self.time_func())
        return await self.send_viewer_health_report(report)

    async def send_recording_health_report(
        self,
        report: RecordingHealthReport,
        *,
        event: str = "recording_health_failed",
    ) -> bool:
        if not self.url or report.ok or not report.issues:
            return False

        now = self.time_func()
        if now < _health_alert_suppressed_until:
            logger.info(
                "Webhook alert suppressed during camera apply grace event=%s issue_count=%d reason=%s remaining_sec=%.1f",
                event,
                len(report.issues),
                _health_alert_suppression_reason,
                _health_alert_suppressed_until - now,
            )
            return False
        key = self._dedup_key(report)
        last_sent = self._last_sent_at.get(key)
        if last_sent is not None and now - last_sent < self.cooldown_sec:
            logger.info("Webhook alert suppressed by cooldown key=%s", key)
            return False

        payload = self._payload(report, event=event)
        if self._apply_camera_incident_throttle(payload, report, event=event, domain="recording", now=now):
            return False
        return await self._post_payload(payload, key=key, now=now)

    async def send_viewer_health_report(self, report: ViewerHealthReport) -> bool:
        if not self.url or report.ok or not report.issues:
            return False

        now = self.time_func()
        if now < _health_alert_suppressed_until:
            logger.info(
                "Webhook alert suppressed during camera apply grace event=viewer_health_failed issue_count=%d reason=%s remaining_sec=%.1f",
                len(report.issues),
                _health_alert_suppression_reason,
                _health_alert_suppressed_until - now,
            )
            return False
        key = self._dedup_key(report)
        last_sent = self._last_sent_at.get(key)
        if last_sent is not None and now - last_sent < self.cooldown_sec:
            logger.info("Webhook alert suppressed by cooldown key=%s", key)
            return False

        payload = self._viewer_payload(report)
        if self._apply_camera_incident_throttle(payload, report, event="viewer_health_failed", domain="viewer", now=now):
            return False
        return await self._post_payload(payload, key=key, now=now)
