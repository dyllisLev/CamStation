import pytest
from httpx import AsyncClient, ASGITransport

@pytest.mark.asyncio
async def test_get_settings_returns_defaults(tmp_path, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", str(tmp_path / "t.db"))
    import importlib, database, main
    importlib.reload(database)
    importlib.reload(main)
    await database.init_db()
    async with AsyncClient(transport=ASGITransport(main.app), base_url="http://test") as c:
        r = await c.get("/api/settings")
    assert r.status_code == 200
    data = r.json()
    assert data["retention_days"] == 30
    assert data["segment_minutes"] == 10

@pytest.mark.asyncio
async def test_post_settings_updates_value(tmp_path, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", str(tmp_path / "t.db"))
    import importlib, database, main
    importlib.reload(database)
    importlib.reload(main)
    await database.init_db()
    async with AsyncClient(transport=ASGITransport(main.app), base_url="http://test") as c:
        r = await c.post("/api/settings", json={"retention_days": 14, "segment_minutes": 5,
                                                  "motion_threshold": 0.03, "max_storage_gb": 100})
        assert r.status_code == 200
        r2 = await c.get("/api/settings")
        assert r2.json()["retention_days"] == 14


@pytest.mark.asyncio
async def test_viewer_version_not_found(tmp_path, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", str(tmp_path / "t.db"))
    monkeypatch.setenv("VIEWER_DIR", str(tmp_path / "nonexistent"))
    import importlib, database, main
    importlib.reload(database)
    importlib.reload(main)
    await database.init_db()
    async with AsyncClient(transport=ASGITransport(main.app), base_url="http://test") as c:
        r = await c.get("/api/settings/viewer-version")
    assert r.status_code == 404


@pytest.mark.asyncio
async def test_viewer_version_returns_version(tmp_path, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", str(tmp_path / "t.db"))
    monkeypatch.setenv("VIEWER_DIR", str(tmp_path))
    (tmp_path / "version.txt").write_text("1.2.3")
    import importlib, database, main
    importlib.reload(database)
    importlib.reload(main)
    await database.init_db()
    async with AsyncClient(transport=ASGITransport(main.app), base_url="http://test") as c:
        r = await c.get("/api/settings/viewer-version")
    assert r.status_code == 200
    assert r.json() == {"version": "1.2.3"}


@pytest.mark.asyncio
async def test_viewer_app_not_found(tmp_path, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", str(tmp_path / "t.db"))
    monkeypatch.setenv("VIEWER_DIR", str(tmp_path / "nonexistent"))
    import importlib, database, main
    importlib.reload(database)
    importlib.reload(main)
    await database.init_db()
    async with AsyncClient(transport=ASGITransport(main.app), base_url="http://test") as c:
        r = await c.get("/api/settings/viewer-app")
    assert r.status_code == 404


@pytest.mark.asyncio
async def test_viewer_app_download(tmp_path, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", str(tmp_path / "t.db"))
    monkeypatch.setenv("VIEWER_DIR", str(tmp_path))
    (tmp_path / "version.txt").write_text("1.0.0")
    (tmp_path / "CamViewer.exe").write_bytes(b"MZ_fake_exe")
    import importlib, database, main
    importlib.reload(database)
    importlib.reload(main)
    await database.init_db()
    async with AsyncClient(transport=ASGITransport(main.app), base_url="http://test") as c:
        r = await c.get("/api/settings/viewer-app")
    assert r.status_code == 200
    assert r.headers["content-type"] == "application/octet-stream"
    assert r.content == b"MZ_fake_exe"
