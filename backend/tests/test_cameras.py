import pytest
import httpx
import respx
from httpx import AsyncClient, ASGITransport

@pytest.mark.asyncio
async def test_get_cameras_returns_list():
    with respx.mock:
        respx.get("http://127.0.0.1:1984/api/streams").mock(
            return_value=httpx.Response(200, json={
                "camera-yard":  {"producers": [{"url": "rtsp://x"}]},
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
