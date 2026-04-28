import pytest
import aiosqlite
from routers.timeline import scan_segments, get_motion_events

@pytest.fixture
def recording_tree(tmp_path):
    cam_dir = tmp_path / "camera-yard" / "2026-04-27"
    cam_dir.mkdir(parents=True)
    (cam_dir / "10-00.mp4").touch()
    (cam_dir / "10-10.mp4").touch()
    (cam_dir / "10-20.mp4").touch()
    return str(tmp_path)

@pytest.mark.asyncio
async def test_scan_segments_finds_files(recording_tree):
    segments = await scan_segments("camera-yard", "2026-04-27", recording_tree)
    assert len(segments) == 3
    filenames = [s["filename"] for s in segments]
    assert "10-00.mp4" in filenames

@pytest.mark.asyncio
async def test_scan_segments_empty_on_missing_date(recording_tree):
    segments = await scan_segments("camera-yard", "2026-01-01", recording_tree)
    assert segments == []

@pytest.mark.asyncio
async def test_get_motion_events(tmp_path, monkeypatch):
    db_file = str(tmp_path / "test.db")
    monkeypatch.setenv("CAMSTATION_DB_PATH", db_file)
    import importlib, database
    importlib.reload(database)
    await database.init_db()

    async with aiosqlite.connect(db_file) as db:
        await db.execute(
            "INSERT INTO motion_events(camera_id, ts_start, ts_end) VALUES(?,?,?)",
            ("camera-yard", 1777248000.0, 1777248005.0)
        )
        await db.commit()

    events = await get_motion_events("camera-yard", "2026-04-27", db_file)
    assert len(events) == 1
    assert events[0]["camera_id"] == "camera-yard"
