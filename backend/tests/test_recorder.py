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
