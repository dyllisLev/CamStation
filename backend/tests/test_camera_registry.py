from pathlib import Path

import aiosqlite
import pytest

pytestmark = pytest.mark.anyio


def _write_go2rtc_config(path: Path):
    path.write_text(
        """
streams:
  cam1: rtsp://user:secret@example/cam1
  cam1_sub: rtsp://user:secret@example/cam1-sub
# disabled 2026-05-15: 임시 비활성화
#   cam2: rtsp://user:secret@example/cam2
api:
  listen: "127.0.0.1:1984"
""".lstrip(),
        encoding="utf-8",
    )


async def test_import_cameras_from_go2rtc_config_creates_camera_rows(test_db, tmp_path):
    from services.camera_importer import import_cameras_from_go2rtc_config

    config = tmp_path / "go2rtc.yaml"
    _write_go2rtc_config(config)

    result = await import_cameras_from_go2rtc_config(str(config), db_path=test_db)

    assert result.created == 2
    assert result.skipped == 0

    async with aiosqlite.connect(test_db) as db:
        db.row_factory = aiosqlite.Row
        rows = await db.execute_fetchall(
            "SELECT id, display_name, enabled, main_stream_url, sub_stream_url "
            "FROM cameras ORDER BY sort_order"
        )

    assert [row["id"] for row in rows] == ["cam1", "cam2"]
    assert rows[0]["display_name"] == "cam1"
    assert rows[0]["enabled"] == 1
    assert rows[0]["main_stream_url"] == "rtsp://user:secret@example/cam1"
    assert rows[0]["sub_stream_url"] == "rtsp://user:secret@example/cam1-sub"
    assert rows[1]["enabled"] == 0


async def test_camera_registry_lists_admin_items_without_leaking_stream_urls(test_db, tmp_path):
    from services.camera_importer import import_cameras_from_go2rtc_config
    from services.camera_registry import list_camera_admin_items

    config = tmp_path / "go2rtc.yaml"
    _write_go2rtc_config(config)
    await import_cameras_from_go2rtc_config(str(config), db_path=test_db)

    cameras = await list_camera_admin_items(db_path=test_db)

    assert [camera.id for camera in cameras] == ["cam1", "cam2"]
    assert cameras[0].display_name == "cam1"
    assert cameras[0].enabled is True
    assert cameras[0].has_sub is True
    assert cameras[0].main_stream_configured is True
    assert cameras[0].sub_stream_configured is True
    assert cameras[0].onvif_configured is False
    assert not hasattr(cameras[0], "main_stream_url")


async def test_camera_registry_enabled_camera_ids_excludes_disabled_and_archived(test_db, tmp_path):
    from services.camera_importer import import_cameras_from_go2rtc_config
    from services.camera_registry import get_enabled_camera_ids

    config = tmp_path / "go2rtc.yaml"
    _write_go2rtc_config(config)
    await import_cameras_from_go2rtc_config(str(config), db_path=test_db)

    async with aiosqlite.connect(test_db) as db:
        await db.execute("UPDATE cameras SET archived_at=123 WHERE id='cam1'")
        await db.commit()

    assert await get_enabled_camera_ids(db_path=test_db) == []
