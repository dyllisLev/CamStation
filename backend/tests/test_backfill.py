import pytest
import aiosqlite
from pathlib import Path


@pytest.fixture
def anyio_backend():
    return 'asyncio'


@pytest.mark.asyncio
async def test_parse_ts_start_new_format():
    from services.backfill import _parse_ts_start
    ts = _parse_ts_start("14-30.mp4", "2026-04-28")
    assert ts is not None
    # KST 2026-04-28 14:30 = UTC 05:30 = 1777354200
    assert 1777354000 < ts < 1777355000


@pytest.mark.asyncio
async def test_parse_ts_start_old_format():
    from services.backfill import _parse_ts_start
    ts = _parse_ts_start("2026-04-28_14-30.mp4", "2026-04-28")
    assert ts is not None
    assert 1777354000 < ts < 1777355000


@pytest.mark.asyncio
async def test_parse_ts_start_invalid_returns_none():
    from services.backfill import _parse_ts_start
    assert _parse_ts_start("garbage.mp4", "2026-04-28") is None


@pytest.mark.anyio
async def test_backfill_inserts_rows(tmp_path, test_db):
    from services.backfill import backfill_recordings

    # 파일 생성
    day_dir = tmp_path / "recordings" / "cam1" / "2026-04-28"
    day_dir.mkdir(parents=True)
    (day_dir / "14-30.mp4").write_bytes(b"x" * 1000)
    (day_dir / "14-40.mp4").write_bytes(b"x" * 2000)

    await backfill_recordings(str(tmp_path / "recordings"), test_db, active_cam_ids=[])

    async with aiosqlite.connect(test_db) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute("SELECT * FROM recordings ORDER BY ts_start")
        rows = [dict(r) for r in await cur.fetchall()]

    assert len(rows) == 2
    assert rows[0]["filename"] == "14-30.mp4"
    assert rows[0]["camera_id"] == "cam1"
    assert rows[0]["ts_end"] == pytest.approx(rows[1]["ts_start"])
    assert rows[0]["file_size"] == 1000
    assert rows[1]["ts_end"] is not None  # 마지막 파일, 현재 녹화 중 아님 → mtime 사용


@pytest.mark.anyio
async def test_backfill_last_file_null_if_active(tmp_path, test_db):
    from services.backfill import backfill_recordings
    import datetime

    today = datetime.datetime.now().strftime("%Y-%m-%d")
    day_dir = tmp_path / "recordings" / "cam1" / today
    day_dir.mkdir(parents=True)
    (day_dir / "00-00.mp4").write_bytes(b"x" * 500)
    (day_dir / "00-10.mp4").write_bytes(b"x" * 600)

    await backfill_recordings(str(tmp_path / "recordings"), test_db, active_cam_ids=["cam1"])

    async with aiosqlite.connect(test_db) as db:
        cur = await db.execute(
            "SELECT ts_end FROM recordings WHERE filename='00-10.mp4'"
        )
        row = await cur.fetchone()
    assert row[0] is None  # 현재 녹화 중 → ts_end NULL


@pytest.mark.anyio
async def test_backfill_idempotent(tmp_path, test_db):
    from services.backfill import backfill_recordings

    day_dir = tmp_path / "recordings" / "cam1" / "2026-04-28"
    day_dir.mkdir(parents=True)
    (day_dir / "10-00.mp4").write_bytes(b"x" * 100)

    await backfill_recordings(str(tmp_path / "recordings"), test_db, active_cam_ids=[])
    await backfill_recordings(str(tmp_path / "recordings"), test_db, active_cam_ids=[])

    async with aiosqlite.connect(test_db) as db:
        cur = await db.execute("SELECT COUNT(*) FROM recordings")
        count = (await cur.fetchone())[0]
    assert count == 1
