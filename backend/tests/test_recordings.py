import pytest
import aiosqlite


@pytest.mark.anyio
async def test_list_recordings_returns_rows(test_db, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", test_db)
    async with aiosqlite.connect(test_db) as db:
        # 2026-04-28 14:30 KST
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start, ts_end, file_size) VALUES(?,?,?,?,?)",
            ("cam1", "14-30.mp4", 1777354200.0, 1777354800.0, 50000),
        )
        # 2026-04-27 14:30 KST (전날, should be excluded)
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start, ts_end, file_size) VALUES(?,?,?,?,?)",
            ("cam1", "14-30-prev.mp4", 1777267800.0, 1777268400.0, 40000),
        )
        await db.commit()

    from routers.recordings import list_recordings
    rows = await list_recordings("cam1", "2026-04-28")
    assert len(rows) == 1
    assert rows[0]["filename"] == "14-30.mp4"
    assert rows[0]["ts_end"] == pytest.approx(1777354800.0)
    assert rows[0]["file_size"] == 50000


@pytest.mark.anyio
async def test_list_recordings_empty(test_db, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", test_db)
    from routers.recordings import list_recordings
    rows = await list_recordings("cam1", "2026-01-01")
    assert rows == []


@pytest.mark.anyio
async def test_list_recordings_invalid_date(test_db, monkeypatch):
    monkeypatch.setenv("CAMSTATION_DB_PATH", test_db)
    from fastapi import HTTPException
    from routers.recordings import list_recordings
    with pytest.raises(HTTPException) as exc_info:
        await list_recordings("cam1", "not-a-date")
    assert exc_info.value.status_code == 400
