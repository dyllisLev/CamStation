import hmac
import hashlib
import json

import pytest

pytestmark = pytest.mark.anyio


@pytest.fixture(autouse=True)
def reset_webhook_alert_grace():
    import services.webhook_alerts as webhook_alerts
    webhook_alerts._health_alert_suppressed_until = 0.0
    webhook_alerts._health_alert_suppression_reason = ""
    yield
    webhook_alerts._health_alert_suppressed_until = 0.0
    webhook_alerts._health_alert_suppression_reason = ""


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


async def test_webhook_alert_sender_suppresses_health_alerts_during_camera_apply_grace():
    import services.webhook_alerts as webhook_alerts
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.viewer_health import ViewerHealthIssue, ViewerHealthReport

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append(content)
        class Resp:
            def raise_for_status(self):
                return None
        return Resp()

    now = [1000.0]
    webhook_alerts._health_alert_suppressed_until = 1060.0
    webhook_alerts._health_alert_suppression_reason = "camera cam1 enabled=True"
    sender = webhook_alerts.WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
        time_func=lambda: now[0],
    )
    recording_report = RecordingHealthReport(
        ok=False,
        checked_at=1000,
        camera_count=8,
        active_count=7,
        issues=[RecordingHealthIssue("recording_process_failed", "ERROR", "cam1", "ffmpeg exited")],
    )
    viewer_report = ViewerHealthReport(
        ok=False,
        checked_at=1000,
        client_count=1,
        issues=[ViewerHealthIssue(
            code="viewer_stream_degraded",
            severity="ERROR",
            client_id="viewer-1",
            message="partial",
            name="NUC",
            age_sec=1,
            expected_cameras=8,
            healthy_cameras=0,
            app_version="1.0.4",
            pid=1234,
        )],
    )

    assert await sender.send_recording_health_report(recording_report, event="recording_process_failed") is False
    assert await sender.send_viewer_health_report(viewer_report) is False
    assert calls == []

    now[0] = 1061.0
    assert await sender.send_viewer_health_report(viewer_report) is True
    assert len(calls) == 1

    webhook_alerts._health_alert_suppressed_until = 0.0
    webhook_alerts._health_alert_suppression_reason = ""


async def test_camera_source_incident_suppresses_derived_viewer_and_recording_alerts():
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.viewer_health import ViewerHealthIssue, ViewerHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append(json.loads(content.decode()))
        class Resp:
            def raise_for_status(self):
                return None
        return Resp()

    now = [1000.0]
    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
        time_func=lambda: now[0],
        cooldown_sec=300,
        camera_incident_summary_sec=3600,
    )
    viewer_report = ViewerHealthReport(
        ok=False,
        checked_at=1000,
        client_count=1,
        issues=[
            ViewerHealthIssue(
                code="viewer_camera_not_receiving",
                severity="ERROR",
                client_id="viewer-1",
                message="camera-site-2_sub stalled",
                camera_id="camera-site-2_sub",
                expected_cameras=8,
                healthy_cameras=7,
            ),
            ViewerHealthIssue(
                code="viewer_stream_degraded",
                severity="ERROR",
                client_id="viewer-1",
                message="7/8",
                expected_cameras=8,
                healthy_cameras=7,
            ),
        ],
    )
    recording_report = RecordingHealthReport(
        ok=False,
        checked_at=1020,
        camera_count=8,
        active_count=7,
        issues=[RecordingHealthIssue("recording_process_failed", "ERROR", "camera-site-2", "ffmpeg exited")],
    )

    assert await sender.send_viewer_health_report(viewer_report) is True
    now[0] = 1020.0
    assert await sender.send_recording_health_report(recording_report, event="recording_process_failed") is False
    assert len(calls) == 1
    assert calls[0]["event"] == "viewer_health_failed"
    assert calls[0]["incident"]["camera_id"] == "camera-site-2"
    assert calls[0]["incident"]["root_cause"] == "camera_source_or_network_unreachable"

    now[0] = 4701.0
    assert await sender.send_recording_health_report(recording_report, event="recording_process_failed") is True
    assert len(calls) == 2
    assert calls[1]["incident"]["camera_id"] == "camera-site-2"
    assert "recording_process_failed" in calls[1]["incident"]["events"]


async def test_camera_source_incident_sends_recovery_after_observed_domains_ok():
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.viewer_health import ViewerHealthIssue, ViewerHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append(json.loads(content.decode()))
        class Resp:
            def raise_for_status(self):
                return None
        return Resp()

    now = [1000.0]
    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
        time_func=lambda: now[0],
        camera_incident_summary_sec=3600,
    )
    viewer_report = ViewerHealthReport(
        ok=False,
        checked_at=1000,
        client_count=1,
        issues=[ViewerHealthIssue("viewer_camera_not_receiving", "ERROR", "viewer-1", "stalled", camera_id="camera-site-2_sub")],
    )
    recording_report = RecordingHealthReport(
        ok=False,
        checked_at=1020,
        camera_count=8,
        active_count=7,
        issues=[RecordingHealthIssue("recording_process_failed", "ERROR", "camera-site-2", "ffmpeg exited")],
    )

    assert await sender.send_viewer_health_report(viewer_report) is True
    now[0] = 1020.0
    assert await sender.send_recording_health_report(recording_report, event="recording_process_failed") is False

    now[0] = 2000.0
    ok_viewer = ViewerHealthReport(ok=True, checked_at=2000, client_count=1, issues=[])
    assert await sender.observe_viewer_health_report(ok_viewer) is False
    assert len(calls) == 1

    now[0] = 2060.0
    ok_recording = RecordingHealthReport(ok=True, checked_at=2060, camera_count=8, active_count=8, issues=[])
    assert await sender.observe_recording_health_report(ok_recording) is True
    assert len(calls) == 2
    assert calls[1]["event"] == "camera_source_recovered"
    assert calls[1]["severity"] == "INFO"
    assert calls[1]["incident"]["camera_id"] == "camera-site-2"


async def test_camera_source_incident_closes_silently_when_camera_disabled():
    from services.recording_health import RecordingHealthIssue, RecordingHealthReport
    from services.viewer_health import ViewerHealthIssue, ViewerHealthReport
    from services.webhook_alerts import WebhookAlertSender

    calls = []

    async def fake_post(url, *, content, headers, timeout):
        calls.append(json.loads(content.decode()))
        class Resp:
            def raise_for_status(self):
                return None
        return Resp()

    now = [1000.0]
    sender = WebhookAlertSender(
        url="http://hermes:8644/webhooks/camstation-alert",
        secret="secret",
        post_func=fake_post,
        time_func=lambda: now[0],
        camera_incident_summary_sec=3600,
        get_enabled_camera_ids=lambda: ["camera-site-1", "camera-site-3", "camera-site-4", "camera-pen", "camera-yard", "camera-storage-1", "camera-storage-2"],
    )
    viewer_report = ViewerHealthReport(
        ok=False,
        checked_at=1000,
        client_count=1,
        issues=[ViewerHealthIssue("viewer_camera_not_receiving", "ERROR", "viewer-1", "stalled", camera_id="camera-site-2_sub")],
    )
    recording_report = RecordingHealthReport(
        ok=False,
        checked_at=1020,
        camera_count=8,
        active_count=7,
        issues=[RecordingHealthIssue("recording_process_failed", "ERROR", "camera-site-2", "ffmpeg exited")],
    )

    assert await sender.send_viewer_health_report(viewer_report) is True
    now[0] = 1020.0
    assert await sender.send_recording_health_report(recording_report, event="recording_process_failed") is False

    now[0] = 2000.0
    ok_viewer = ViewerHealthReport(ok=True, checked_at=2000, client_count=1, issues=[])
    assert await sender.observe_viewer_health_report(ok_viewer) is False

    now[0] = 2060.0
    ok_recording = RecordingHealthReport(ok=True, checked_at=2060, camera_count=7, active_count=7, issues=[])
    assert await sender.observe_recording_health_report(ok_recording) is False
    assert len(calls) == 1
    assert calls[0]["event"] == "viewer_health_failed"
