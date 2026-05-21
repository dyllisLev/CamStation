import hmac
import hashlib
import json

import pytest

pytestmark = pytest.mark.anyio


async def test_webhook_alert_sender_posts_hmac_signed_payload():
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append((url, content, headers, timeout))
        class Resp:
            status_code = 202
            text = "accepted"
            def raise_for_status(self):
                return None
        return Resp()

    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
        cooldown_sec=300,
    )
    report = RecordingHealthReport(
        ok=False,
        checked_at=1778800000,
        camera_count=1,
        active_count=1,
        issues=[RecordingHealthIssue(
            code="segment_not_moved",
            severity="ERROR",
            camera_id="cam1",
            message="stale temp",
            path="/tmp/a.mp4",
            filename="a.mp4",
            age_sec=7200,
            file_size=123,
        )],
    )

    delivered = await sender.send_recording_health_report(report)

    assert delivered is True
    assert len(calls) == 1
    url, content, headers, timeout = calls[0]
    assert url == "http://hermes:8644/webhooks/camstation-alert"
    assert timeout == 5
    assert headers["Content-Type"] == "application/json"
    expected_sig = hmac.new(b"secret", content, hashlib.sha256).hexdigest()
    assert headers["X-Webhook-Signature"] == expected_sig
    payload = json.loads(content.decode())
    assert payload["service"] == "camstation-backend"
    assert payload["event"] == "recording_health_failed"
    assert payload["severity"] == "ERROR"
    assert payload["issues"][0]["code"] == "segment_not_moved"


async def test_webhook_alert_sender_posts_viewer_health_payload():
    from services.viewer_health import ViewerHealthIssue, ViewerHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append((url, content, headers, timeout))
        class Resp:
            def raise_for_status(self):
                return None
        return Resp()

    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
    )
    report = ViewerHealthReport(
        ok=False,
        checked_at=1778800000,
        client_count=1,
        issues=[ViewerHealthIssue(
            code="viewer_heartbeat_stale",
            severity="ERROR",
            client_id="viewer-1",
            message="stale",
            name="NUC",
            age_sec=120,
            expected_cameras=8,
            healthy_cameras=8,
            app_version="1.0.4",
            pid=1234,
        )],
    )

    delivered = await sender.send_viewer_health_report(report)

    assert delivered is True
    payload = json.loads(calls[0][1].decode())
    assert payload["event"] == "viewer_health_failed"
    assert payload["severity"] == "ERROR"
    assert payload["client_count"] == 1
    assert payload["issues"][0]["code"] == "viewer_heartbeat_stale"


async def test_webhook_alert_sender_deduplicates_within_cooldown():
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append(content)
        class Resp:
            status_code = 202
            text = "accepted"
            def raise_for_status(self):
                return None
        return Resp()

    now = [1000.0]
    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
        cooldown_sec=300,
        time_func=lambda: now[0],
    )
    report = RecordingHealthReport(
        ok=False,
        checked_at=1000,
        camera_count=1,
        active_count=1,
        issues=[RecordingHealthIssue("db_open_segment_stale", "ERROR", "cam1", "stale")],
    )

    assert await sender.send_recording_health_report(report) is True
    assert await sender.send_recording_health_report(report) is False
    now[0] = 1301.0
    assert await sender.send_recording_health_report(report) is True
    assert len(calls) == 2


async def test_webhook_alert_sender_noops_without_url_or_error():
    from services.recording_health import RecordingHealthReport
    from services.webhook_alerts import WebhookAlertSender

    sender = WebhookAlertSender(url="", secret="secret")
    report = RecordingHealthReport(ok=False, checked_at=1, camera_count=0, active_count=0, issues=[])
    assert await sender.send_recording_health_report(report) is False

    sender = WebhookAlertSender(url="http://x", secret="secret")
    ok_report = RecordingHealthReport(ok=True, checked_at=1, camera_count=0, active_count=0, issues=[])
    assert await sender.send_recording_health_report(ok_report) is False


async def test_webhook_alert_sender_uses_recording_event_override():
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append(content)
        class Resp:
            def raise_for_status(self):
                return None
        return Resp()

    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
    )
    report = RecordingHealthReport(
        ok=False,
        checked_at=1778800000,
        camera_count=1,
        active_count=1,
        issues=[RecordingHealthIssue("recording_process_failed", "ERROR", "cam1", "ffmpeg exited")],
    )

    assert await sender.send_recording_health_report(report, event="recording_process_failed") is True
    payload = json.loads(calls[0].decode())
    assert payload["event"] == "recording_process_failed"
    assert payload["issues"][0]["code"] == "recording_process_failed"
