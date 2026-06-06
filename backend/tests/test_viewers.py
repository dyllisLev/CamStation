import time

import aiosqlite
import pytest

pytestmark = pytest.mark.anyio


async def test_viewer_heartbeat_registers_client_and_camera_health(client):
    payload = {
        "client_id": "viewer-1",
        "name": "거실 모니터",
        "app_version": "1.0.2",
        "server_url": "http://10.0.0.26",
        "platform": "win32",
        "hostname": "living-room-pc",
        "pid": 1234,
        "started_at": 1778800000.0,
        "expected_cameras": 2,
        "cameras": [
            {
                "camera_id": "camera-site-1_sub",
                "connected": True,
                "video_ready_state": 4,
                "last_binary_at": 1778800010.0,
                "last_video_time": 12.5,
                "last_video_time_at": 1778800010.0,
                "stalled_ms": 0,
                "reconnect_count": 1,
            },
            {
                "camera_id": "camera-site-2_sub",
                "connected": False,
                "video_ready_state": 0,
                "last_binary_at": 1778799900.0,
                "last_video_time": 0,
                "last_video_time_at": 1778799900.0,
                "stalled_ms": 120_000,
                "reconnect_count": 7,
                "error": "stalled",
            },
        ],
    }

    r = await client.post("/api/viewers/heartbeat", json=payload)
    assert r.status_code == 200
    body = r.json()
    assert body["client_id"] == "viewer-1"
    assert body["state"] == "degraded"
    assert body["healthy_cameras"] == 1
    assert body["expected_cameras"] == 2

    listed = await client.get("/api/viewers")
    assert listed.status_code == 200
    clients = listed.json()
    assert len(clients) == 1
    assert clients[0]["client_id"] == "viewer-1"
    assert clients[0]["hostname"] == "living-room-pc"
    assert clients[0]["state"] == "degraded"
    assert clients[0]["cameras"][1]["camera_id"] == "camera-site-2_sub"


async def test_viewer_heartbeat_marks_healthy_when_all_cameras_are_receiving(client):
    r = await client.post("/api/viewers/heartbeat", json={
        "client_id": "viewer-ok",
        "name": "정상 뷰어",
        "expected_cameras": 2,
        "cameras": [
            {"camera_id": "cam1_sub", "connected": True, "video_ready_state": 4, "stalled_ms": 0},
            {"camera_id": "cam2_sub", "connected": True, "video_ready_state": 3, "stalled_ms": 500},
        ],
    })

    assert r.status_code == 200
    assert r.json()["state"] == "healthy"
    assert r.json()["healthy_cameras"] == 2


async def test_viewer_heartbeat_treats_recent_stream_activity_as_healthy_even_when_ready_state_is_low(client):
    now = time.time()
    r = await client.post("/api/viewers/heartbeat", json={
        "client_id": "viewer-ready-low",
        "name": "수신중 뷰어",
        "expected_cameras": 1,
        "cameras": [{
            "camera_id": "cam1_sub",
            "connected": True,
            "video_ready_state": 1,
            "last_binary_at": now,
            "last_video_time_at": now,
            "stalled_ms": 0,
        }],
    })

    assert r.status_code == 200
    assert r.json()["state"] == "healthy"
    assert r.json()["healthy_cameras"] == 1


async def test_viewer_remote_command_lifecycle(client):
    await client.post("/api/viewers/heartbeat", json={
        "client_id": "viewer-cmd",
        "name": "명령 테스트",
        "expected_cameras": 1,
        "cameras": [{"camera_id": "cam1_sub", "connected": True, "video_ready_state": 4}],
    })

    created = await client.post("/api/viewers/viewer-cmd/commands", json={
        "command": "reload_page",
        "reason": "수신 정지 복구",
    })
    assert created.status_code == 201
    command = created.json()
    assert command["id"] > 0
    assert command["status"] == "pending"
    assert command["command"] == "reload_page"

    pending = await client.get("/api/viewers/viewer-cmd/commands/pending")
    assert pending.status_code == 200
    pending_body = pending.json()
    assert pending_body["id"] == command["id"]
    assert pending_body["status"] == "claimed"

    completed = await client.post(
        f"/api/viewers/viewer-cmd/commands/{command['id']}/complete",
        json={"ok": True, "message": "reloaded"},
    )
    assert completed.status_code == 200
    assert completed.json()["status"] == "completed"

    no_pending = await client.get("/api/viewers/viewer-cmd/commands/pending")
    assert no_pending.status_code == 204


async def test_rejects_unknown_viewer_command(client):
    await client.post("/api/viewers/heartbeat", json={
        "client_id": "viewer-cmd2",
        "name": "명령 테스트2",
        "expected_cameras": 0,
        "cameras": [],
    })

    r = await client.post("/api/viewers/viewer-cmd2/commands", json={"command": "format_disk"})
    assert r.status_code == 422


async def test_viewer_db_retry_retries_locked_operation(monkeypatch):
    from routers import viewers

    sleeps = []
    attempts = {"count": 0}

    async def fake_sleep(delay):
        sleeps.append(delay)

    async def flaky_operation():
        attempts["count"] += 1
        if attempts["count"] == 1:
            raise aiosqlite.OperationalError("database is locked")
        return "ok"

    monkeypatch.setattr(viewers.asyncio, "sleep", fake_sleep)

    assert await viewers._with_db_retry(flaky_operation, label="test") == "ok"
    assert attempts["count"] == 2
    assert sleeps == [viewers.VIEWER_DB_RETRY_BASE_DELAY_SEC]
