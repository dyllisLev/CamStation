import pytest
from pathlib import Path
from datetime import datetime, timedelta
from services.cleaner import delete_expired_segments, quarantine_stale_temp_segments

@pytest.mark.asyncio
async def test_delete_old_segments(tmp_path):
    cam_dir = tmp_path / "camera-yard"
    old_date = (datetime.now() - timedelta(days=31)).strftime("%Y-%m-%d")
    new_date = datetime.now().strftime("%Y-%m-%d")
    (cam_dir / old_date).mkdir(parents=True)
    (cam_dir / old_date / "10-00.mp4").touch()
    (cam_dir / new_date).mkdir(parents=True)
    (cam_dir / new_date / "10-00.mp4").touch()

    await delete_expired_segments(str(tmp_path), retention_days=30)

    assert not (cam_dir / old_date / "10-00.mp4").exists()
    assert (cam_dir / new_date / "10-00.mp4").exists()


@pytest.mark.asyncio
async def test_quarantine_stale_temp_segments_moves_old_orphans(tmp_path):
    temp_dir = tmp_path / "temp"
    recordings_dir = tmp_path / "recordings"
    quarantine_dir = tmp_path / "quarantine"
    old = temp_dir / "cam1" / "2026-05-22" / "2026-05-23_00-00.mp4"
    old.parent.mkdir(parents=True)
    old.write_bytes(b"old")
    old_mtime = (datetime.now().timestamp() - 10 * 86400)
    old.touch()
    import os
    os.utime(old, (old_mtime, old_mtime))

    recent = temp_dir / "cam1" / datetime.now().strftime("%Y-%m-%d") / "current.mp4"
    recent.parent.mkdir(parents=True)
    recent.write_bytes(b"current")

    moved = await quarantine_stale_temp_segments(
        str(temp_dir),
        str(recordings_dir),
        stale_days=7,
        quarantine_root=str(quarantine_dir),
    )

    assert moved == 1
    assert not old.exists()
    assert recent.exists()
    assert list(quarantine_dir.glob("*/cam1/2026-05-22/2026-05-23_00-00.mp4"))


@pytest.mark.asyncio
async def test_quarantine_stale_temp_segments_keeps_smaller_final_recovery_material(tmp_path):
    temp_dir = tmp_path / "temp"
    recordings_dir = tmp_path / "recordings"
    old = temp_dir / "cam1" / "2026-05-22" / "2026-05-23_00-00.mp4"
    final = recordings_dir / "cam1" / "2026-05-23" / old.name
    old.parent.mkdir(parents=True)
    final.parent.mkdir(parents=True)
    old.write_bytes(b"larger-temp")
    final.write_bytes(b"small")
    old_mtime = (datetime.now().timestamp() - 10 * 86400)
    import os
    os.utime(old, (old_mtime, old_mtime))

    moved = await quarantine_stale_temp_segments(str(temp_dir), str(recordings_dir), stale_days=7)

    assert moved == 0
    assert old.exists()
