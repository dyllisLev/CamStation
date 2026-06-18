import hashlib
import hmac
import json
import logging
from dataclasses import asdict
from typing import Awaitable, Callable

import httpx

from services.recording_health import RecordingHealthReport
from services.viewer_health import ViewerHealthReport

logger = logging.getLogger(__name__)


def suppress_health_alerts_for_camera_apply(*, seconds: int, reason: str = "camera registry apply") -> None:
    """Stub — suppression removed. All alerts pass through to AI for judgment."""
    logger.info("suppress_health_alerts_for_camera_apply called but suppression is disabled (reason=%s)", reason)


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
        post_func: Callable[..., Awaitable[object]] | None = None,
        time_func: Callable[[], float] | None = None,
        get_enabled_camera_ids=None,
    ):
        self.url = url
        self.secret = secret
        self.post_func = post_func or self._httpx_post
        self.time_func = time_func or __import__("time").time
        self.get_enabled_camera_ids = get_enabled_camera_ids

    async def _httpx_post(self, url: str, *, content: bytes, headers: dict[str, str], timeout: int):
        async with httpx.AsyncClient(timeout=timeout) as client:
            return await client.post(url, content=content, headers=headers)

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
            "message": f"CamStation recording event: {len(report.issues)} issue(s)" if event != "recording_health_failed" else f"CamStation recording health check: {len(report.issues)} issue(s)",
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
            "message": f"CamStation viewer health check: {len(report.issues)} issue(s)",
            "checked_at": report.checked_at,
            "client_count": report.client_count,
            "issues": [asdict(i) for i in report.issues],
        }

    async def _post_payload(self, payload: dict) -> bool:
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
            logger.info("Webhook alert delivered event=%s issue_count=%d", payload["event"], len(payload.get("issues", [])))
            return True
        except Exception as e:
            logger.warning("Webhook alert delivery failed url=%s error=%s", self.url, e)
            return False

    async def observe_recording_health_report(self, report: RecordingHealthReport) -> bool:
        if not report.ok and report.issues:
            return await self.send_recording_health_report(report)
        return False

    async def observe_viewer_health_report(self, report: ViewerHealthReport) -> bool:
        if not report.ok and report.issues:
            return await self.send_viewer_health_report(report)
        return False

    async def send_recording_health_report(
        self,
        report: RecordingHealthReport,
        *,
        event: str = "recording_health_failed",
    ) -> bool:
        if not self.url or report.ok or not report.issues:
            return False
        payload = self._payload(report, event=event)
        return await self._post_payload(payload)

    async def send_viewer_health_report(self, report: ViewerHealthReport) -> bool:
        if not self.url or report.ok or not report.issues:
            return False
        payload = self._viewer_payload(report)
        return await self._post_payload(payload)
