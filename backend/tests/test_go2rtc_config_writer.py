from pathlib import Path

import aiosqlite
import pytest

pytestmark = pytest.mark.anyio


async def _insert_camera(db_path: str, **overrides):
    values = {
        "id": "cam1",
        "display_name": "마당",
        "location": None,
        "enabled": 1,
        "archived_at": None,
        "main_stream_url": "rtsp://user:secret@example/cam1",
        "sub_stream_url": "rtsp://user:secret@example/cam1-sub",
        "sort_order": 0,
        "created_at": 123.0,
        "updated_at": 123.0,
    }
    values.update(overrides)
    async with aiosqlite.connect(db_path) as db:
        await db.execute(
            """
            INSERT INTO cameras(
                id, display_name, location, enabled, archived_at,
                main_stream_url, sub_stream_url, sort_order, created_at, updated_at
            ) VALUES(:id, :display_name, :location, :enabled, :archived_at,
                     :main_stream_url, :sub_stream_url, :sort_order, :created_at, :updated_at)
            """,
            values,
        )
        await db.commit()


async def test_render_go2rtc_config_from_db_preserves_non_stream_sections(test_db, tmp_path):
    from services.go2rtc_config_writer import render_go2rtc_config_from_db

    await _insert_camera(test_db)
    await _insert_camera(
        test_db,
        id="cam2",
        display_name="비활성",
        enabled=0,
        main_stream_url="rtsp://user:secret@example/cam2",
        sub_stream_url=None,
        sort_order=1,
    )
    existing = tmp_path / "go2rtc.yaml"
    existing.write_text(
        """
api:
  listen: "127.0.0.1:1984"
streams:
  stale: rtsp://old/stale
webrtc:
  candidates:
    - 127.0.0.1:8555
""".lstrip(),
        encoding="utf-8",
    )

    rendered = await render_go2rtc_config_from_db(str(existing), db_path=test_db)

    assert "api:\n  listen" in rendered
    assert "webrtc:\n  candidates" in rendered
    assert "  stale:" not in rendered
    assert "  cam1: rtsp://user:secret@example/cam1" in rendered
    assert "  cam1_sub: rtsp://user:secret@example/cam1-sub" in rendered
    assert "cam2" not in rendered


async def test_write_go2rtc_config_from_db_creates_backup_and_atomic_output(test_db, tmp_path):
    from services.go2rtc_config_writer import write_go2rtc_config_from_db

    await _insert_camera(test_db)
    config = tmp_path / "go2rtc.yaml"
    config.write_text("streams:\n  old: rtsp://old\napi:\n  listen: old\n", encoding="utf-8")

    result = await write_go2rtc_config_from_db(str(config), db_path=test_db)

    assert result.changed is True
    assert result.backup_path is not None
    assert Path(result.backup_path).exists()
    text = config.read_text(encoding="utf-8")
    assert "  old:" not in text
    assert "  cam1: rtsp://user:secret@example/cam1" in text
    assert "api:\n  listen: old" in text


async def test_write_go2rtc_config_from_db_reports_unchanged(test_db, tmp_path):
    from services.go2rtc_config_writer import write_go2rtc_config_from_db

    await _insert_camera(test_db)
    config = tmp_path / "go2rtc.yaml"
    config.write_text(
        "streams:\n"
        "  cam1: rtsp://user:secret@example/cam1\n"
        "  cam1_sub: rtsp://user:secret@example/cam1-sub\n",
        encoding="utf-8",
    )

    result = await write_go2rtc_config_from_db(str(config), db_path=test_db)

    assert result.changed is False
    assert result.backup_path is None
