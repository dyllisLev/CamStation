import pytest
import aiosqlite
import os
from database import init_db, get_db_path, get_setting, set_setting

@pytest.fixture
def tmp_db(tmp_path, monkeypatch):
    db_file = str(tmp_path / "test.db")
    monkeypatch.setenv("CAMSTATION_DB_PATH", db_file)
    return db_file

@pytest.mark.asyncio
async def test_init_db_creates_tables(tmp_db):
    await init_db()
    async with aiosqlite.connect(tmp_db) as db:
        cur = await db.execute(
            "SELECT name FROM sqlite_master WHERE type='table'"
        )
        tables = {row[0] for row in await cur.fetchall()}
    assert "motion_events" in tables
    assert "settings" in tables

@pytest.mark.asyncio
async def test_default_settings_inserted(tmp_db):
    await init_db()
    async with aiosqlite.connect(tmp_db) as db:
        cur = await db.execute("SELECT key, value FROM settings")
        rows = {row[0]: row[1] for row in await cur.fetchall()}
    assert rows["retention_days"] == "30"
    assert rows["segment_minutes"] == "10"
    assert rows["motion_threshold"] == "0.02"

@pytest.mark.asyncio
async def test_get_setting_returns_default(tmp_db):
    await init_db()
    value = await get_setting("retention_days")
    assert value == "30"

@pytest.mark.asyncio
async def test_get_setting_returns_none_for_missing_key(tmp_db):
    await init_db()
    value = await get_setting("nonexistent_key")
    assert value is None

@pytest.mark.asyncio
async def test_set_and_get_setting_roundtrip(tmp_db):
    await init_db()
    await set_setting("retention_days", "14")
    value = await get_setting("retention_days")
    assert value == "14"

@pytest.mark.asyncio
async def test_init_db_idempotent(tmp_db):
    await init_db()
    await set_setting("retention_days", "99")
    # second init_db should NOT overwrite user-modified setting
    await init_db()
    value = await get_setting("retention_days")
    assert value == "99"
