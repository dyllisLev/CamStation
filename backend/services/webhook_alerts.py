import hashlib
import hmac
import json
import logging
import time
from dataclasses import asdict
from typing import Awaitable, Callable

import httpx

from services.recording_health import RecordingHealthReport
from services.viewer_health import ViewerHealthReport

logger = logging.getLogger(__name__)

_health_alert_suppressed_until = 0.0
_health_alert_suppression_reason = ""


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


class WebhookAlertSender:
    def __init__(
        self,
        *,
        url: str,
        secret: str,
        cooldown_sec: int = 300,
        post_func: Callable[..., Awaitable[object]] | None = None,
        time_func: Callable[[], float] | None = None,
    ):
        self.url = url
        self.secret = secret
        self.cooldown_sec = cooldown_sec
        self.post_func = post_func or self._httpx_post
        self.time_func = time_func or time.time
        self._last_sent_at: dict[str, float] = {}

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
            self._last_sent_at[key] = now
            logger.info("Webhook alert delivered event=%s issue_count=%d", payload["event"], len(report.issues))
            return True
        except Exception as e:
            logger.warning("Webhook alert delivery failed url=%s error=%s", self.url, e)
            return False

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
            self._last_sent_at[key] = now
            logger.info("Webhook alert delivered event=%s issue_count=%d", payload["event"], len(report.issues))
            return True
        except Exception as e:
            logger.warning("Webhook alert delivery failed url=%s error=%s", self.url, e)
            return False
