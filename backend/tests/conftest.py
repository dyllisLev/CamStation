import os
import pytest
import aiosqlite

pytest_plugins = ('anyio',)


@pytest.fixture
async def test_db(tmp_path):
    """Create a fresh SQLite DB with the layouts table for each test."""
    db_path = str(tmp_path / "test.db")
    async with aiosqlite.connect(db_path) as db:
        await db.execute("""
            CREATE TABLE IF NOT EXISTS layouts (
                id         TEXT PRIMARY KEY,
                name       TEXT NOT NULL,
                data       TEXT NOT NULL,
                created_at INTEGER NOT NULL,
                updated_at INTEGER NOT NULL
            )
        """)
        await db.commit()
    return db_path


@pytest.fixture
async def client(test_db, monkeypatch):
    # get_db_path() reads CAMSTATION_DB_PATH env var at call time — patch it here
    monkeypatch.setenv("CAMSTATION_DB_PATH", test_db)

    from main import app
    from httpx import AsyncClient, ASGITransport
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as c:
        yield c
