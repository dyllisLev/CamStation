import json

import aiosqlite
import pytest

pytestmark = pytest.mark.anyio


async def _insert_viewer(db_path, **kwargs):
    payload = kwargs.pop("payload", {})
    values = {
        "client_id": "viewer-1",
        "name": "NUC",
        "app_version": "1.0.4",
        "server_url": "http://10.0.0.26",
        "platform": "win32",
        "hostname": "NUC",
        "pid": 1234,
        "started_at": 1000.0,
        "last_seen": 1990.0,
        "expected_cameras": 2,
        "healthy_cameras": 2,
        "state": "healthy",
        "payload_json": json.dumps(payload, ensure_ascii=False),
    }
    values.update(kwargs)
    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            """
            INSERT INTO viewer_clients(
                client_id, name, app_version, server_url, platform, hostname, pid,
                started_at, last_seen, expected_cameras, healthy_cameras, state, payload_json
            ) VALUES(:client_id,:name,:app_version,:server_url,:platform,:hostname,:pid,
                     :started_at,:last_seen,:expected_cameras,:healthy_cameras,:state,:payload_json)
            """,
            values,
        )
        await db.commit()


async def test_viewer_health_reports_missing_viewer(test_db):
    from services.viewer_health import check_viewer_health

    report = await check_viewer_health(test_db, now_ts=2000, max_heartbeat_age_sec=60)

    assert report.ok is False
    assert report.client_count == 0
    assert report.issues[0].code == "viewer_missing"


async def test_viewer_health_reports_stale_heartbeat_as_exe_not_running(test_db):
    from services.viewer_health import check_viewer_health

    await _insert_viewer(test_db, last_seen=1900.0)

    report = await check_viewer_health(test_db, now_ts=2000, max_heartbeat_age_sec=60)

    assert report.ok is False
    issue = report.issues[0]
    assert issue.code == "viewer_heartbeat_stale"
    assert issue.client_id == "viewer-1"
    assert issue.pid == 1234


async def test_viewer_health_reports_degraded_stream_reception(test_db):
    from services.viewer_health import check_viewer_health

    await _insert_viewer(
        test_db,
        healthy_cameras=1,
        state="degraded",
        payload={
            "cameras": [
                {"camera_id": "cam1_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
                {"camera_id": "cam2_sub", "connected": False, "video_ready_state": 0, "stalled_ms": 120000, "error": "stalled"},
            ]
        },
    )

    report = await check_viewer_health(test_db, now_ts=2000, max_heartbeat_age_sec=60)

    assert report.ok is False
    codes = {i.code for i in report.issues}
    assert "viewer_stream_degraded" in codes
    assert "viewer_camera_not_receiving" in codes
    assert any(i.camera_id == "cam2_sub" for i in report.issues)


async def test_viewer_health_ok_when_ready_state_low_but_stream_activity_is_recent(test_db):
    from services.viewer_health import check_viewer_health

    await _insert_viewer(
        test_db,
        healthy_cameras=2,
        state="healthy",
        payload={
            "cameras": [
                {
                    "camera_id": "cam1_sub",
                    "connected": True,
                    "video_ready_state": 1,
                    "last_binary_at": 1995.0,
                    "last_video_time_at": 1994.0,
                    "stalled_ms": 0,
                },
                {"camera_id": "cam2_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
            ]
        },
    )

    report = await check_viewer_health(test_db, now_ts=2000, max_heartbeat_age_sec=60)

    assert report.ok is True
    assert report.issues == []


async def test_viewer_health_reports_low_ready_state_without_recent_activity(test_db):
    from services.viewer_health import check_viewer_health

    await _insert_viewer(
        test_db,
        healthy_cameras=1,
        state="degraded",
        payload={
            "cameras": [
                {
                    "camera_id": "cam1_sub",
                    "connected": True,
                    "video_ready_state": 1,
                    "last_binary_at": 1900.0,
                    "last_video_time_at": 1900.0,
                    "stalled_ms": 0,
                },
                {"camera_id": "cam2_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
            ]
        },
    )

    report = await check_viewer_health(test_db, now_ts=2000, max_heartbeat_age_sec=60)

    assert report.ok is False
    assert any(i.camera_id == "cam1_sub" for i in report.issues)


async def test_viewer_health_ok_when_heartbeat_recent_and_all_streams_healthy(test_db):
    from services.viewer_health import check_viewer_health

    await _insert_viewer(
        test_db,
        payload={
            "cameras": [
                {"camera_id": "cam1_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
                {"camera_id": "cam2_sub", "connected": True, "video_ready_state": 3, "stalled_ms": 100},
            ]
        },
    )

    report = await check_viewer_health(test_db, now_ts=2000, max_heartbeat_age_sec=60)

    assert report.ok is True
    assert report.issues == []


async def test_viewer_health_ignores_disabled_camera_payload(test_db):
    from services.viewer_health import check_viewer_health

    await _insert_viewer(
        test_db,
        expected_cameras=2,
        healthy_cameras=1,
        state="degraded",
        payload={
            "cameras": [
                {"camera_id": "cam1_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
                {"camera_id": "cam2_sub", "connected": False, "video_ready_state": 0, "stalled_ms": 120000, "error": "stalled"},
            ]
        },
    )

    report = await check_viewer_health(
        test_db,
        now_ts=2000,
        max_heartbeat_age_sec=60,
        enabled_camera_ids=["cam1"],
    )

    assert report.ok is True
    assert report.issues == []


async def test_viewer_event_notifier_debounces_repeated_degraded_heartbeats(test_db):
    import asyncio
    import time
    from services.viewer_health import ViewerHealthEventNotifier

    await _insert_viewer(
        test_db,
        last_seen=time.time(),
        healthy_cameras=1,
        state="degraded",
        payload={
            "cameras": [
                {"camera_id": "cam1_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
                {"camera_id": "cam2_sub", "connected": False, "video_ready_state": 0, "stalled_ms": 120000, "error": "stalled"},
            ]
        },
    )

    sent = []

    class FakeSender:
        async def send_viewer_health_report(self, report):
            sent.append(report)
            return True

    notifier = ViewerHealthEventNotifier(
        test_db,
        alert_sender=FakeSender(),
        max_heartbeat_age_sec=60,
        debounce_sec=0.02,
    )
    try:
        notifier.notify_heartbeat(
            client_id="viewer-1",
            state="degraded",
            previous_state="degraded",
            healthy_cameras=1,
            previous_healthy_cameras=1,
        )
        notifier.notify_heartbeat(
            client_id="viewer-1",
            state="degraded",
            previous_state="degraded",
            healthy_cameras=1,
            previous_healthy_cameras=1,
        )
        await asyncio.sleep(0.08)
    finally:
        await notifier.close()

    assert len(sent) == 1
    assert sent[0].ok is False
    assert any(i.code == "viewer_camera_not_receiving" for i in sent[0].issues)
