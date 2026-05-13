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
    assert "%Y-%m-%d_%H-%M.mp4" in joined

def test_ffmpeg_cmd_no_video_transcoding():
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/camera-yard",
        output_dir="/tmp",
        segment_minutes=10,
    )
    assert "libx264" not in cmd
    assert "-c:v" in cmd and "copy" in cmd
    assert "-c:a" in cmd and "aac" in cmd


def test_ffmpeg_cmd_disables_progress_stats_on_stderr():
    """ffmpeg의 줄바꿈 없는 진행상태 출력이 stderr watcher를 죽이지 않게 한다."""
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/camera-yard",
        output_dir="/tmp",
        segment_minutes=10,
    )

    assert "-nostats" in cmd
    assert cmd.index("-nostats") < cmd.index("-i")


def test_ffmpeg_cmd_filename_includes_date():
    """파일명이 YYYY-MM-DD_HH-MM.mp4 형식이어야 한다."""
    from services.recorder import build_ffmpeg_cmd
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/cam1",
        output_dir="/recordings/cam1/2026-04-28",
        segment_minutes=10,
    )
    pattern = [p for p in cmd if p.endswith(".mp4")][0]
    import os
    assert os.path.basename(pattern) == "%Y-%m-%d_%H-%M.mp4"


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
    # 신형 YYYY-MM-DD_HH-MM 형식
    ts = _ts_from_path("/recordings/cam1/2026-04-28/2026-04-28_14-30.mp4")
    assert ts is not None
    assert 1777354000 < ts < 1777355000


def test_ts_from_path_legacy_format():
    from services.recorder import _ts_from_path
    # 구형 HH-MM 형식 (하위 호환)
    ts = _ts_from_path("/recordings/cam1/2026-04-28/14-30.mp4")
    assert ts is not None
    assert 1777354000 < ts < 1777355000


def test_ts_from_path_invalid():
    from services.recorder import _ts_from_path
    assert _ts_from_path("/recordings/cam1/2026-04-28/garbage.mp4") is None


def test_next_delay_resets_on_success():
    from services.recorder import _next_delay
    # ran >= 30 → delay 5s로 리셋
    assert _next_delay(60, 30.0) == 5
    assert _next_delay(60, 60.0) == 5
    assert _next_delay(10, 30.0) == 5

def test_next_delay_doubles_on_fast_fail():
    from services.recorder import _next_delay
    assert _next_delay(5, 0.5) == 10
    assert _next_delay(10, 2.0) == 20
    assert _next_delay(20, 1.0) == 40

def test_next_delay_caps_at_max():
    from services.recorder import _next_delay
    assert _next_delay(40, 1.0) == 60   # 40*2=80 → cap 60
    assert _next_delay(60, 1.0) == 60   # already at max

def test_next_delay_boundary():
    from services.recorder import _next_delay
    assert _next_delay(20, 29.9) == 40  # below threshold → double
    assert _next_delay(20, 30.0) == 5   # at threshold exactly → reset


def test_date_from_segment_path_prefers_filename_date():
    from services.recorder import _date_from_segment_path

    assert _date_from_segment_path(
        "/opt/camstation/temp/camera-yard/2026-05-12/2026-05-13_10-00.mp4"
    ) == "2026-05-13"


def test_date_from_segment_path_falls_back_to_parent_for_legacy_filename():
    from services.recorder import _date_from_segment_path

    assert _date_from_segment_path(
        "/opt/camstation/temp/camera-yard/2026-05-12/10-00.mp4"
    ) == "2026-05-12"


@pytest.mark.asyncio
async def test_terminate_process_kills_when_terminate_does_not_exit():
    from services.recorder import _terminate_process

    class HangingProc:
        def __init__(self):
            self.returncode = None
            self.terminated = False
            self.killed = False

        def terminate(self):
            self.terminated = True

        def kill(self):
            self.killed = True
            self.returncode = -9

        async def wait(self):
            if not self.killed:
                import asyncio
                await asyncio.sleep(10)
            return self.returncode

    proc = HangingProc()
    await _terminate_process(proc, timeout=0.01)

    assert proc.terminated is True
    assert proc.killed is True
