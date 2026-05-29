import aiosqlite
import pytest

pytestmark = pytest.mark.anyio


async def test_camera_admin_apply_restarts_go2rtc_reconciles_recorders_and_reloads_viewers(client, test_db, tmp_path, monkeypatch):
    import routers.camera_admin as camera_admin
    import services.camera_runtime_apply as runtime_apply

    config_path = tmp_path / "go2rtc.yaml"
    config_path.write_text("streams: {}\n", encoding="utf-8")
    monkeypatch.setattr(camera_admin, "GO2RTC_CONFIG", str(config_path))

    async with aiosqlite.connect(test_db) as db:
        await db.execute(
            """
            INSERT INTO cameras(id, display_name, enabled, main_stream_url, sub_stream_url, sort_order, created_at, updated_at)
            VALUES('cam1', 'Cam 1', 1, 'rtsp://example/cam1', 'rtsp://example/cam1-sub', 1, 1, 1)
            """
        )
        await db.execute(
            """
            INSERT INTO viewer_clients(client_id, name, app_version, server_url, platform, hostname, pid, started_at, last_seen, expected_cameras, healthy_cameras, state, payload_json)
            VALUES('viewer-1', 'Viewer', '1.0.0', null, null, null, 123, 1, 999, 1, 1, 'healthy', '{}')
            """
        )
        await db.commit()

    calls = []

    async def fake_restart_go2rtc():
        calls.append(("restart_go2rtc",))

    async def fake_reconcile_recorders(camera_ids, sub_camera_ids):
        calls.append(("reconcile_recorders", list(camera_ids), list(sub_camera_ids)))

    def fake_suppress_health_alerts_for_camera_apply(*, seconds, reason):
        calls.append(("suppress", seconds, reason))

    monkeypatch.setattr(runtime_apply, "restart_go2rtc", fake_restart_go2rtc)
    monkeypatch.setattr(runtime_apply, "reconcile_recorders", fake_reconcile_recorders)
    monkeypatch.setattr(camera_admin, "suppress_health_alerts_for_camera_apply", fake_suppress_health_alerts_for_camera_apply)

    response = await client.post("/api/camera-admin/apply")

    assert response.status_code == 200
    assert response.json()["changed"] is True
    assert response.json()["go2rtc_restarted"] is True
    assert response.json()["recorders_reconciled"] is True
    assert response.json()["viewer_reload_commands"] == 1
    assert ("restart_go2rtc",) in calls
    assert ("reconcile_recorders", ["cam1"], ["cam1"]) in calls
    assert ("suppress", 120, "camera registry manual apply") in calls

    async with aiosqlite.connect(test_db) as db:
        rows = await db.execute_fetchall(
            "SELECT client_id, command, status, reason FROM viewer_commands"
        )
    assert rows == [("viewer-1", "reload_page", "pending", "camera registry applied")]


async def test_camera_admin_apply_skips_runtime_when_config_unchanged(client, test_db, tmp_path, monkeypatch):
    import routers.camera_admin as camera_admin
    import services.camera_runtime_apply as runtime_apply

    config_path = tmp_path / "go2rtc.yaml"
    config_path.write_text(
        """
streams:
  cam1: rtsp://example/cam1
""".lstrip(),
        encoding="utf-8",
    )
    monkeypatch.setattr(camera_admin, "GO2RTC_CONFIG", str(config_path))

    async with aiosqlite.connect(test_db) as db:
        await db.execute(
            """
            INSERT INTO cameras(id, display_name, enabled, main_stream_url, sort_order, created_at, updated_at)
            VALUES('cam1', 'Cam 1', 1, 'rtsp://example/cam1', 1, 1, 1)
            """
        )
        await db.commit()

    async def fail_restart_go2rtc():
        raise AssertionError("restart should not run")

    async def fail_reconcile_recorders(camera_ids, sub_camera_ids):
        raise AssertionError("reconcile should not run")

    monkeypatch.setattr(runtime_apply, "restart_go2rtc", fail_restart_go2rtc)
    monkeypatch.setattr(runtime_apply, "reconcile_recorders", fail_reconcile_recorders)

    response = await client.post("/api/camera-admin/apply")

    assert response.status_code == 200
    assert response.json()["changed"] is False
    assert response.json()["go2rtc_restarted"] is False
    assert response.json()["recorders_reconciled"] is False
    assert response.json()["viewer_reload_commands"] == 0
