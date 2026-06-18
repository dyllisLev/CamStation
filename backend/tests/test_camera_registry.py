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


async def test_camera_registry_lists_admin_items_with_editable_connection_values(test_db, tmp_path):
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
    assert cameras[0].main_stream_url == "rtsp://user:secret@example/cam1"
    assert cameras[0].sub_stream_url == "rtsp://user:secret@example/cam1-sub"


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


def _write_go2rtc_config_list_format(path: Path):
    """go2rtc YAML list format (production format) with - source entries."""
    path.write_text(
        """
streams:
  cam1:
    - rtsp://user:secret@example/cam1
    - on_demand: false
  cam1_sub:
    - ffmpeg:rtsp://127.0.0.1:8554/cam1#video=h264
    - on_demand: false
  cam2:
    - rtsp://user:secret@example/cam2
    - on_demand: false
api:
  listen: "127.0.0.1:1984"
""".lstrip(),
        encoding="utf-8",
    )


async def test_import_skips_yaml_list_items_in_list_format(test_db, tmp_path):
    """YAML list items (- rtsp://, - on_demand:, - ffmpeg:) must not create camera rows."""
    from services.camera_importer import import_cameras_from_go2rtc_config

    config = tmp_path / "go2rtc.yaml"
    _write_go2rtc_config_list_format(config)

    result = await import_cameras_from_go2rtc_config(str(config), db_path=test_db)

    assert result.created == 2
    assert result.skipped == 0

    async with aiosqlite.connect(test_db) as db:
        rows = await db.execute_fetchall(
            "SELECT id, enabled, main_stream_url FROM cameras ORDER BY sort_order"
        )

    ids = [row[0] for row in rows]
    assert ids == ["cam1", "cam2"]
    assert rows[0][2] == "rtsp://user:secret@example/cam1"
    # No garbage entries like "- rtsp", "- on_demand", "- ffmpeg"
    assert "- rtsp" not in ids
    assert "- on_demand" not in ids
    assert "- ffmpeg" not in ids


async def test_list_camera_configs_ignores_yaml_list_items():
    """list_camera_configs must not return cameras from YAML list items."""
    from services.camera_config import list_camera_configs
    import tempfile

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write(
            "streams:\n"
            "  cam1:\n"
            "    - rtsp://user:pass@host/stream1\n"
            "    - on_demand: false\n"
            "  cam1_sub:\n"
            "    - ffmpeg:rtsp://127.0.0.1:8554/cam1#video=h264\n"
            "    - on_demand: false\n"
        )
        f.flush()
        cameras = list_camera_configs(f.name)

    ids = [c.id for c in cameras]
    assert ids == ["cam1"]
    assert "- rtsp" not in ids
    assert "- on_demand" not in ids
    assert "- ffmpeg" not in ids


async def test_import_skips_yaml_list_items_as_camera_names(test_db, tmp_path):
    """Regression: go2rtc YAML list items like '- rtsp://...', '- on_demand: false',
    '- ffmpeg:...' must not be parsed as camera names."""
    from services.camera_config import list_camera_configs
    from services.camera_importer import import_cameras_from_go2rtc_config

    config = tmp_path / "go2rtc.yaml"
    config.write_text(
        """
streams:
  real-cam:
    - rtsp://user:secret@192.0.2.1:554/stream1
    - on_demand: false
  real-cam_sub:
    - ffmpeg:rtsp://127.0.0.1:8554/real-cam#video=h264
    - on_demand: false
api:
  listen: "127.0.0.1:1984"
""".lstrip(),
        encoding="utf-8",
    )

    cameras = list_camera_configs(str(config))
    assert [c.id for c in cameras] == ["real-cam"]

    result = await import_cameras_from_go2rtc_config(str(config), db_path=test_db)
    assert result.created == 1

    async with aiosqlite.connect(test_db) as db:
        rows = await db.execute_fetchall("SELECT id FROM cameras ORDER BY id")
    assert [r[0] for r in rows] == ["real-cam"]
