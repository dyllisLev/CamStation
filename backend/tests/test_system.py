import pytest
import httpx
import respx
from fastapi import FastAPI
from httpx import AsyncClient, ASGITransport

pytestmark = pytest.mark.anyio


@pytest.fixture
async def client():
    from routers.system import router
    app = FastAPI()
    app.include_router(router)
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as c:
        yield c


async def test_health(client):
    r = await client.get("/api/system/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}


async def test_version_missing_files(client, tmp_path, monkeypatch):
    import routers.system as sys_mod
    monkeypatch.setattr(sys_mod, "VERSION_FILE", str(tmp_path / "no-version"))
    monkeypatch.setattr(sys_mod, "TOKEN_FILE", str(tmp_path / "no-token"))

    r = await client.get("/api/system/version")
    assert r.status_code == 200
    body = r.json()
    assert body["current_version"] == "unknown"
    assert body["latest_version"] is None
    assert body["update_available"] is False


async def test_version_up_to_date(client, tmp_path, monkeypatch):
    import routers.system as sys_mod

    version_file = tmp_path / ".current-version"
    version_file.write_text("v20260428-abc1234")
    token_file = tmp_path / ".github-token"
    token_file.write_text("fake-token")

    monkeypatch.setattr(sys_mod, "VERSION_FILE", str(version_file))
    monkeypatch.setattr(sys_mod, "TOKEN_FILE", str(token_file))
    monkeypatch.setattr(sys_mod, "GITHUB_REPO", "test/repo")

    with respx.mock:
        respx.get("https://api.github.com/repos/test/repo/releases/latest").mock(
            return_value=httpx.Response(200, json={"tag_name": "v20260428-abc1234"})
        )
        r = await client.get("/api/system/version")

    assert r.status_code == 200
    body = r.json()
    assert body["current_version"] == "v20260428-abc1234"
    assert body["latest_version"] == "v20260428-abc1234"
    assert body["update_available"] is False


async def test_version_update_available(client, tmp_path, monkeypatch):
    import routers.system as sys_mod

    version_file = tmp_path / ".current-version"
    version_file.write_text("v20260427-abc1234")
    token_file = tmp_path / ".github-token"
    token_file.write_text("fake-token")

    monkeypatch.setattr(sys_mod, "VERSION_FILE", str(version_file))
    monkeypatch.setattr(sys_mod, "TOKEN_FILE", str(token_file))
    monkeypatch.setattr(sys_mod, "GITHUB_REPO", "test/repo")

    with respx.mock:
        respx.get("https://api.github.com/repos/test/repo/releases/latest").mock(
            return_value=httpx.Response(200, json={"tag_name": "v20260428-def5678"})
        )
        r = await client.get("/api/system/version")

    assert r.status_code == 200
    body = r.json()
    assert body["current_version"] == "v20260427-abc1234"
    assert body["latest_version"] == "v20260428-def5678"
    assert body["update_available"] is True


async def test_trigger_update_starts(client, monkeypatch):
    import routers.system as sys_mod
    monkeypatch.setattr(sys_mod, "DEPLOY_SCRIPT", "/bin/true")
    monkeypatch.setattr(sys_mod, "_update_running", False)

    r = await client.post("/api/system/update")
    assert r.status_code == 200
    assert r.json()["status"] == "started"


async def test_trigger_update_blocks_concurrent(client, monkeypatch):
    import routers.system as sys_mod
    monkeypatch.setattr(sys_mod, "_update_running", True)

    r = await client.post("/api/system/update")
    assert r.status_code == 200
    assert r.json()["status"] == "already_running"
