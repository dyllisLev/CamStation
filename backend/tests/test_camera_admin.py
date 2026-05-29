import aiosqlite
import httpx
import pytest
import respx

pytestmark = pytest.mark.anyio


async def test_camera_admin_lists_registry_cameras_with_connection_values(client, test_db):
    async with aiosqlite.connect(test_db) as db:
        now = 123.0
        await db.execute(
            """
            INSERT INTO cameras(
                id, display_name, location, enabled, main_stream_url, sub_stream_url,
                sort_order, notes, created_at, updated_at
            ) VALUES(?,?,?,?,?,?,?,?,?,?)
            """,
            (
                "cam1",
                "마당",
                "집",
                1,
                "rtsp://user:secret@example/cam1",
                "rtsp://user:secret@example/cam1-sub",
                10,
                "테스트",
                now,
                now,
            ),
        )
        await db.commit()

    with respx.mock:
        respx.get("http://127.0.0.1:1984/api/streams").mock(
            return_value=httpx.Response(200, json={
                "cam1": {"producers": [{"id": 1}]},
                "cam1_sub": {"producers": [{"id": 2}]},
            })
        )
        response = await client.get("/api/camera-admin")

    assert response.status_code == 200
    body = response.json()
    assert body == [{
        "id": "cam1",
        "display_name": "마당",
        "location": "집",
        "enabled": True,
        "archived": False,
        "online": True,
        "has_sub": True,
        "main_stream_configured": True,
        "sub_stream_configured": True,
        "onvif_configured": False,
        "main_stream_url": "rtsp://user:secret@example/cam1",
        "sub_stream_url": "rtsp://user:secret@example/cam1-sub",
        "onvif_host": None,
        "onvif_port": None,
        "onvif_username": None,
        "onvif_password": None,
        "sort_order": 10,
        "notes": "테스트",
    }]


async def test_camera_admin_apply_writes_go2rtc_config_from_registry(client, test_db, tmp_path, monkeypatch):
    import routers.camera_admin as camera_admin_router
    import services.camera_runtime_apply as runtime_apply

    async def noop_restart_go2rtc():
        return None

    async def noop_reconcile_recorders(camera_ids, sub_camera_ids):
        return None

    async def noop_enqueue_viewer_reload_commands(reason="camera registry applied"):
        return 0

    monkeypatch.setattr(runtime_apply, "restart_go2rtc", noop_restart_go2rtc)
    monkeypatch.setattr(runtime_apply, "reconcile_recorders", noop_reconcile_recorders)
    monkeypatch.setattr(runtime_apply, "enqueue_viewer_reload_commands", noop_enqueue_viewer_reload_commands)

    config = tmp_path / "go2rtc.yaml"
    config.write_text("streams:\n  old: rtsp://old\napi:\n  listen: old\n", encoding="utf-8")
    monkeypatch.setattr(camera_admin_router, "GO2RTC_CONFIG", str(config))

    async with aiosqlite.connect(test_db) as db:
        now = 123.0
        await db.execute(
            """
            INSERT INTO cameras(
                id, display_name, enabled, main_stream_url, sub_stream_url,
                sort_order, created_at, updated_at
            ) VALUES(?,?,?,?,?,?,?,?)
            """,
            (
                "cam1",
                "마당",
                1,
                "rtsp://user:secret@example/cam1",
                None,
                0,
                now,
                now,
            ),
        )
        await db.commit()

    response = await client.post("/api/camera-admin/apply")

    assert response.status_code == 200
    body = response.json()
    assert body["changed"] is True
    assert body["backup_created"] is True
    text = config.read_text(encoding="utf-8")
    assert "  old:" not in text
    assert "  cam1: rtsp://user:secret@example/cam1" in text
    assert "secret" not in response.text


async def test_camera_admin_creates_camera_and_returns_editable_connection_values(client, test_db):
    response = await client.post(
        "/api/camera-admin",
        json={
            "id": "cam-new",
            "display_name": "새 카메라",
            "location": "집",
            "enabled": True,
            "main_stream_url": "rtsp://user:secret@example/new-main",
            "sub_stream_url": "rtsp://user:secret@example/new-sub",
            "onvif_host": "192.0.2.10",
            "onvif_port": 80,
            "onvif_username": "admin",
            "onvif_password": "pw-secret",
            "sort_order": 5,
            "notes": "신규",
        },
    )

    assert response.status_code == 201
    assert response.json()["id"] == "cam-new"
    assert response.json()["display_name"] == "새 카메라"
    assert response.json()["sub_stream_configured"] is True
    assert response.json()["onvif_configured"] is True
    assert response.json()["main_stream_url"] == "rtsp://user:secret@example/new-main"
    assert response.json()["sub_stream_url"] == "rtsp://user:secret@example/new-sub"
    assert response.json()["onvif_password"] == "pw-secret"

    async with aiosqlite.connect(test_db) as db:
        rows = await db.execute_fetchall(
            "SELECT main_stream_url, onvif_password FROM cameras WHERE id='cam-new'"
        )
    assert rows == [("rtsp://user:secret@example/new-main", "pw-secret")]


async def test_camera_admin_updates_camera_without_overwriting_omitted_connection_fields(client, test_db):
    async with aiosqlite.connect(test_db) as db:
        now = 123.0
        await db.execute(
            """
            INSERT INTO cameras(
                id, display_name, enabled, main_stream_url, sub_stream_url,
                onvif_password, sort_order, created_at, updated_at
            ) VALUES(?,?,?,?,?,?,?,?,?)
            """,
            ("cam1", "기존", 1, "rtsp://old-secret/main", None, "old-pw", 0, now, now),
        )
        await db.commit()

    response = await client.patch(
        "/api/camera-admin/cam1",
        json={
            "display_name": "수정됨",
            "location": "camera-site",
            "sub_stream_url": "rtsp://new-secret/sub",
            "notes": "변경",
        },
    )

    assert response.status_code == 200
    assert response.json()["display_name"] == "수정됨"
    assert response.json()["location"] == "camera-site"
    assert response.json()["sub_stream_configured"] is True
    assert response.json()["main_stream_url"] == "rtsp://old-secret/main"
    assert response.json()["sub_stream_url"] == "rtsp://new-secret/sub"
    assert response.json()["onvif_password"] == "old-pw"

    async with aiosqlite.connect(test_db) as db:
        rows = await db.execute_fetchall(
            "SELECT main_stream_url, sub_stream_url, onvif_password FROM cameras WHERE id='cam1'"
        )
    assert rows == [("rtsp://old-secret/main", "rtsp://new-secret/sub", "old-pw")]


async def test_camera_admin_sets_enabled_and_archives_camera(client, test_db):
    async with aiosqlite.connect(test_db) as db:
        now = 123.0
        await db.execute(
            """
            INSERT INTO cameras(
                id, display_name, enabled, main_stream_url, sort_order, created_at, updated_at
            ) VALUES(?,?,?,?,?,?,?)
            """,
            ("cam1", "마당", 1, "rtsp://secret/main", 0, now, now),
        )
        await db.commit()

    disable = await client.post("/api/camera-admin/cam1/enabled", json={"enabled": False})
    assert disable.status_code == 200
    assert disable.json()["enabled"] is False
    assert disable.json()["main_stream_url"] == "rtsp://secret/main"

    archive = await client.delete("/api/camera-admin/cam1")
    assert archive.status_code == 200
    assert archive.json()["archived"] is True
    assert archive.json()["enabled"] is False

    async with aiosqlite.connect(test_db) as db:
        rows = await db.execute_fetchall("SELECT enabled, archived_at FROM cameras WHERE id='cam1'")
    assert rows[0][0] == 0
    assert rows[0][1] is not None
