import pytest
from services.recorder import build_ffmpeg_cmd

def test_ffmpeg_cmd_uses_copy_mode():
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/camera-yard",
        output_dir="/recordings/camera-yard/2026-04-27",
        segment_minutes=10,
    )
    assert "-c:v" in cmd and "copy" in cmd
    assert "-c:a" in cmd and "aac" in cmd
    assert "-segment_time" in cmd
    assert "600" in cmd
    assert "/recordings/camera-yard/2026-04-27" in " ".join(cmd)

def test_ffmpeg_cmd_output_pattern():
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/camera-yard",
        output_dir="/recordings/camera-yard/2026-04-27",
        segment_minutes=10,
    )
    joined = " ".join(cmd)
    assert "%H-%M.mp4" in joined

def test_ffmpeg_cmd_no_video_transcoding():
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/camera-yard",
        output_dir="/tmp",
        segment_minutes=10,
    )
    assert "libx264" not in cmd
    assert "-c:v" in cmd and "copy" in cmd
    assert "-c:a" in cmd and "aac" in cmd


def test_ffmpeg_cmd_filename_only_time():
    """파일명이 HH-MM.mp4 형식이어야 한다 (날짜 없음)."""
    from services.recorder import build_ffmpeg_cmd
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/cam1",
        output_dir="/recordings/cam1/2026-04-28",
        segment_minutes=10,
    )
    pattern = [p for p in cmd if p.endswith(".mp4")][0]
    import os
    assert os.path.basename(pattern) == "%H-%M.mp4"


def test_parse_stderr_line_returns_path():
    from services.recorder import parse_stderr_line
    line = "[segment @ 0xabc] Opening '/recordings/cam1/2026-04-28/14-30.mp4' for writing"
    result = parse_stderr_line(line)
    assert result == "/recordings/cam1/2026-04-28/14-30.mp4"


def test_parse_stderr_line_returns_none_for_other():
    from services.recorder import parse_stderr_line
    assert parse_stderr_line("frame=  120 fps=25.0 q=-1.0 size=   1024kB") is None


def test_ts_from_path_kst():
    from services.recorder import _ts_from_path
    ts = _ts_from_path("/recordings/cam1/2026-04-28/14-30.mp4")
    assert ts is not None
    # KST 2026-04-28 14:30 = Unix timestamp 근처
    assert 1777354000 < ts < 1777355000


def test_ts_from_path_invalid():
    from services.recorder import _ts_from_path
    assert _ts_from_path("/recordings/cam1/2026-04-28/garbage.mp4") is None
