import os
import time

import aiosqlite
import pytest

pytestmark = pytest.mark.anyio


async def _init_recordings_db(path):
    async with aiosqlite.connect(path) as db:
        await db.execute(
            """
            CREATE TABLE recordings (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                camera_id TEXT NOT NULL,
                filename TEXT NOT NULL,
                ts_start REAL NOT NULL,
                ts_end REAL,
                file_size INTEGER,
                created REAL DEFAULT (unixepoch()),
                UNIQUE(camera_id, ts_start)
            )
            """
        )
        await db.commit()


async def test_recording_health_passes_when_active_temp_and_recent_recording_exist(tmp_path):
    from services.recording_health import check_recording_health

    now = time.time()
    db_path = tmp_path / "camstation.db"
    await _init_recordings_db(db_path)

    recordings = tmp_path / "recordings" / "cam1" / "2026-05-15"
    recordings.mkdir(parents=True)
    done = recordings / "2026-05-15_09-00.mp4"
    done.write_bytes(b"video")

    temp = tmp_path / "temp" / "cam1" / "2026-05-15"
    temp.mkdir(parents=True)
    current = temp / "2026-05-15_09-30.mp4"
    current.write_bytes(b"current")
    os.utime(current, (now, now))

    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start, ts_end, file_size) VALUES(?,?,?,?,?)",
            ("cam1", done.name, now - 1800, now - 10, done.stat().st_size),
        )
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start) VALUES(?,?,?)",
            ("cam1", current.name, now - 100),
        )
        await db.commit()

    report = await check_recording_health(
        ["cam1"],
        str(tmp_path / "recordings"),
        str(tmp_path / "temp"),
        str(db_path),
        segment_minutes=30,
        active_cam_ids=["cam1"],
        now_ts=now,
    )

    assert report.ok is True
    assert report.issues == []
    assert report.camera_count == 1


async def test_recording_health_detects_missing_active_recorder(tmp_path):
    from services.recording_health import check_recording_health

    db_path = tmp_path / "camstation.db"
    await _init_recordings_db(db_path)

    report = await check_recording_health(
        ["cam1"],
        str(tmp_path / "recordings"),
        str(tmp_path / "temp"),
        str(db_path),
        segment_minutes=30,
        active_cam_ids=[],
        now_ts=time.time(),
    )

    assert report.ok is False
    assert any(i.code == "recorder_not_active" and i.camera_id == "cam1" for i in report.issues)


async def test_recording_health_detects_stale_temp_file_not_moved(tmp_path):
    from services.recording_health import check_recording_health

    now = time.time()
    db_path = tmp_path / "camstation.db"
    await _init_recordings_db(db_path)

    stale_dir = tmp_path / "temp" / "cam1" / "2026-05-15"
    stale_dir.mkdir(parents=True)
    stale = stale_dir / "2026-05-15_08-00.mp4"
    stale.write_bytes(b"stale")
    old = now - 7200
    os.utime(stale, (old, old))

    report = await check_recording_health(
        ["cam1"],
        str(tmp_path / "recordings"),
        str(tmp_path / "temp"),
        str(db_path),
        segment_minutes=30,
        active_cam_ids=["cam1"],
        now_ts=now,
    )

    assert report.ok is False
    assert any(i.code == "segment_not_moved" and i.path == str(stale) for i in report.issues)


async def test_recording_health_detects_stale_open_db_segment(tmp_path):
    from services.recording_health import check_recording_health

    now = time.time()
    db_path = tmp_path / "camstation.db"
    await _init_recordings_db(db_path)

    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start) VALUES(?,?,?)",
            ("cam1", "2026-05-15_07-00.mp4", now - 7200),
        )
        await db.commit()

    report = await check_recording_health(
        ["cam1"],
        str(tmp_path / "recordings"),
        str(tmp_path / "temp"),
        str(db_path),
        segment_minutes=30,
        active_cam_ids=["cam1"],
        now_ts=now,
    )

    assert report.ok is False
    assert any(i.code == "db_open_segment_stale" and i.filename == "2026-05-15_07-00.mp4" for i in report.issues)


async def test_recording_health_detects_recent_recording_missing_on_disk(tmp_path):
    from services.recording_health import check_recording_health

    now = time.time()
    db_path = tmp_path / "camstation.db"
    await _init_recordings_db(db_path)

    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            "INSERT INTO recordings(camera_id, filename, ts_start, ts_end, file_size) VALUES(?,?,?,?,?)",
            ("cam1", "2026-05-15_09-00.mp4", now - 1800, now - 60, 1234),
        )
        await db.commit()

    report = await check_recording_health(
        ["cam1"],
        str(tmp_path / "recordings"),
        str(tmp_path / "temp"),
        str(db_path),
        segment_minutes=30,
        active_cam_ids=["cam1"],
        now_ts=now,
    )

    assert report.ok is False
    assert any(i.code == "recording_file_missing" for i in report.issues)
