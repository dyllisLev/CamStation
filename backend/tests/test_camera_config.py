from pathlib import Path

import pytest

pytestmark = pytest.mark.anyio


def _write_config(path: Path):
    path.write_text(
        """
streams:
  cam1: rtsp://example/cam1
  cam1_sub: "ffmpeg:rtsp://127.0.0.1:8554/cam1#video=h264"
# disabled 2026-05-15: 임시 비활성화
#   cam2: rtsp://example/cam2
# disabled 2026-05-15: 임시 비활성화
#   cam2_sub: "ffmpeg:rtsp://127.0.0.1:8554/cam2#video=h264"
api:
  listen: "127.0.0.1:1984"
""".lstrip(),
        encoding="utf-8",
    )


async def test_camera_config_lists_enabled_and_disabled_streams(tmp_path):
    from services.camera_config import list_camera_configs

    config = tmp_path / "go2rtc.yaml"
    _write_config(config)

    cameras = list_camera_configs(str(config))

    assert [c.id for c in cameras] == ["cam1", "cam2"]
    assert cameras[0].enabled is True
    assert cameras[0].has_sub is True
    assert cameras[1].enabled is False
    assert cameras[1].has_sub is True


async def test_camera_config_can_disable_and_enable_camera(tmp_path):
    from services.camera_config import list_camera_configs, set_camera_enabled

    config = tmp_path / "go2rtc.yaml"
    _write_config(config)

    changed = set_camera_enabled(str(config), "cam1", False)
    assert changed is True
    cameras = {c.id: c for c in list_camera_configs(str(config))}
    assert cameras["cam1"].enabled is False

    changed = set_camera_enabled(str(config), "cam1", True)
    assert changed is True
    cameras = {c.id: c for c in list_camera_configs(str(config))}
    assert cameras["cam1"].enabled is True


async def test_camera_config_raises_for_unknown_camera(tmp_path):
    from services.camera_config import set_camera_enabled

    config = tmp_path / "go2rtc.yaml"
    _write_config(config)

    with pytest.raises(KeyError):
        set_camera_enabled(str(config), "missing", False)
