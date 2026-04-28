import pytest
import aiosqlite

pytest_plugins = ('anyio',)


@pytest.fixture
async def test_db(tmp_path, monkeypatch):
    db_path = str(tmp_path / "test.db")
    monkeypatch.setenv("CAMSTATION_DB_PATH", db_path)
    import importlib, database
    importlib.reload(database)
    await database.init_db()
    return db_path


@pytest.fixture
async def client(test_db, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", test_db)
    from main import app
    from httpx import AsyncClient, ASGITransport
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as c:
        yield c
