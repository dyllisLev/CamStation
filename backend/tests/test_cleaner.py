import pytest
from pathlib import Path
from datetime import datetime, timedelta
from services.cleaner import delete_expired_segments

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
