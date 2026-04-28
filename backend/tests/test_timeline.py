import pytest
import aiosqlite
from routers.timeline import get_segments_from_db, get_motion_events


@pytest.mark.anyio
async def test_get_segments_from_db_returns_rows(test_db):
    async with aiosqlite.connect(test_db) as db:
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start, ts_end) VALUES(?,?,?,?)",
            ("camera-yard", "14-30.mp4", 1777354200.0, 1777354800.0),
        )
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start, ts_end) VALUES(?,?,?,?)",
            ("camera-yard", "14-40.mp4", 1777354800.0, 1777355400.0),
        )
        await db.commit()

    rows = await get_segments_from_db("camera-yard", "2026-04-28", test_db)
    assert len(rows) == 2
    assert rows[0]["filename"] == "14-30.mp4"
    assert rows[0]["ts_end"] == pytest.approx(1777354800.0)


@pytest.mark.anyio
async def test_get_segments_from_db_empty_on_no_data(test_db):
    rows = await get_segments_from_db("camera-yard", "2026-01-01", test_db)
    assert rows == []


@pytest.mark.anyio
async def test_get_segments_from_db_filters_by_date(test_db):
    async with aiosqlite.connect(test_db) as db:
        # 2026-04-28 14:30 KST
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start) VALUES(?,?,?)",
            ("cam1", "14-30.mp4", 1777354200.0),
        )
        # 2026-04-27 14:30 KST (전날)
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start) VALUES(?,?,?)",
            ("cam1", "14-30-prev.mp4", 1777267800.0),
        )
        await db.commit()

    rows = await get_segments_from_db("cam1", "2026-04-28", test_db)
    assert len(rows) == 1
    assert rows[0]["filename"] == "14-30.mp4"


@pytest.mark.anyio
async def test_get_motion_events(test_db):
    async with aiosqlite.connect(test_db) as db:
        await db.execute(
            "INSERT INTO motion_events(camera_id, ts_start, ts_end) VALUES(?,?,?)",
            ("camera-yard", 1777354200.0, 1777354205.0),
        )
        await db.commit()

    events = await get_motion_events("camera-yard", "2026-04-28", test_db)
    assert len(events) == 1
    assert events[0]["camera_id"] == "camera-yard"
