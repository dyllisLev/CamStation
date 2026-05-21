import pytest
import httpx
import respx
from httpx import AsyncClient, ASGITransport

@pytest.mark.anyio
async def test_get_cameras_returns_list(test_db):
    with respx.mock:
        respx.get("http://127.0.0.1:1984/api/streams").mock(
            return_value=httpx.Response(200, json={
                "camera-yard":  {"producers": [{"id": 1, "url": "rtsp://x"}]},
                "camera-storage-1": {},
            })
        )
        from main import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            r = await client.get("/api/cameras")
        assert r.status_code == 200
        cameras = r.json()
        assert len(cameras) == 2
        madag = next(c for c in cameras if c["id"] == "camera-yard")
        assert madag["online"] is True
        changgo = next(c for c in cameras if c["id"] == "camera-storage-1")
        assert changgo["online"] is False


@pytest.mark.anyio
async def test_get_cameras_uses_camera_registry_when_db_has_camera_rows(test_db):
    import aiosqlite

    async with aiosqlite.connect(test_db) as db:
        now = 123.0
        await db.executemany(
            """
            INSERT INTO cameras(
                id, display_name, enabled, main_stream_url, sub_stream_url,
                sort_order, created_at, updated_at
            ) VALUES(?,?,?,?,?,?,?,?)
            """,
            [
                ("cam1", "마당", 1, "rtsp://secret/cam1", "rtsp://secret/cam1-sub", 0, now, now),
                ("cam2", "비활성", 0, "rtsp://secret/cam2", None, 1, now, now),
            ],
        )
        await db.commit()

    with respx.mock:
        respx.get("http://127.0.0.1:1984/api/streams").mock(
            return_value=httpx.Response(200, json={
                "cam1": {"producers": [{"id": 1}]},
                "cam1_sub": {"producers": [{"id": 2}]},
                "stale": {"producers": [{"id": 3}]},
            })
        )
        from main import app
        async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as client:
            r = await client.get("/api/cameras")

    assert r.status_code == 200
    cameras = r.json()
    assert [camera["id"] for camera in cameras] == ["cam1"]
    assert cameras[0]["name"] == "마당"
    assert cameras[0]["online"] is True
    assert cameras[0]["has_sub"] is True
    assert "secret" not in r.text
