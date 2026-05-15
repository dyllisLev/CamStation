import pytest

pytestmark = pytest.mark.anyio


HEARTBEAT = {
    "client_id": "viewer-1",
    "name": "거실 모니터 PC",
    "app_version": "1.0.2",
    "server_url": "http://10.0.0.26",
    "platform": "win32",
    "hostname": "living-room",
    "pid": 1234,
    "expected_cameras": 2,
    "cameras": [
        {
            "camera_id": "camera-site-1_sub",
            "connected": True,
            "video_ready_state": 4,
            "last_binary_at": 1000.0,
            "last_video_time_at": 1001.0,
            "stalled_ms": 0,
            "reconnect_count": 0,
            "error": None,
        },
        {
            "camera_id": "camera-site-2_sub",
            "connected": False,
            "video_ready_state": 0,
            "last_binary_at": None,
            "last_video_time_at": None,
            "stalled_ms": 45000,
            "reconnect_count": 3,
            "error": "stalled",
        },
    ],
}


async def test_viewer_heartbeat_upserts_client_and_computes_health(client):
    r = await client.post("/api/viewers/heartbeat", json=HEARTBEAT)
    assert r.status_code == 200
    body = r.json()
    assert body["client_id"] == "viewer-1"
    assert body["expected_cameras"] == 2
    assert body["healthy_cameras"] == 1
    assert body["state"] == "degraded"

    listed = await client.get("/api/viewers")
    assert listed.status_code == 200
    clients = listed.json()
    assert len(clients) == 1
    assert clients[0]["name"] == "거실 모니터 PC"
    assert clients[0]["healthy_cameras"] == 1
    assert clients[0]["payload"]["cameras"][1]["camera_id"] == "camera-site-2_sub"


async def test_viewer_state_is_healthy_when_all_cameras_are_receiving(client):
    payload = {
        **HEARTBEAT,
        "client_id": "viewer-ok",
        "cameras": [
            {**HEARTBEAT["cameras"][0], "camera_id": "cam1_sub"},
            {**HEARTBEAT["cameras"][0], "camera_id": "cam2_sub"},
        ],
    }
    r = await client.post("/api/viewers/heartbeat", json=payload)
    assert r.status_code == 200
    assert r.json()["healthy_cameras"] == 2
    assert r.json()["state"] == "healthy"


async def test_queue_poll_and_complete_viewer_command(client):
    await client.post("/api/viewers/heartbeat", json=HEARTBEAT)

    queued = await client.post(
        "/api/viewers/viewer-1/commands",
        json={"command": "reload_page", "reason": "수신 정지"},
    )
    assert queued.status_code == 201
    command_id = queued.json()["id"]
    assert queued.json()["status"] == "pending"

    pending = await client.get("/api/viewers/viewer-1/commands/pending")
    assert pending.status_code == 200
    assert pending.json()[0]["id"] == command_id
    assert pending.json()[0]["status"] == "claimed"

    completed = await client.post(
        f"/api/viewers/viewer-1/commands/{command_id}/complete",
        json={"ok": True, "message": "reloaded"},
    )
    assert completed.status_code == 200
    assert completed.json()["status"] == "completed"

    pending_after = await client.get("/api/viewers/viewer-1/commands/pending")
    assert pending_after.status_code == 200
    assert pending_after.json() == []


async def test_invalid_viewer_command_is_rejected(client):
    r = await client.post(
        "/api/viewers/viewer-1/commands",
        json={"command": "format_disk", "reason": "nope"},
    )
    assert r.status_code == 422
