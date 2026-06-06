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


def test_ffmpeg_cmd_uses_wallclock_timestamps_for_segment_duration():
    """RTSP 원본 PTS 누적으로 MP4 길이가 몇 시간으로 보이지 않도록 wallclock 기준으로 리타임한다."""
    cmd = build_ffmpeg_cmd(
        source_rtsp="rtsp://127.0.0.1:8554/camera-site-1",
        output_dir="/tmp",
        segment_minutes=30,
    )

    assert "-use_wallclock_as_timestamps" in cmd
    assert cmd[cmd.index("-use_wallclock_as_timestamps") + 1] == "1"
    assert cmd.index("-use_wallclock_as_timestamps") < cmd.index("-i")
    assert "-avoid_negative_ts" in cmd
    assert cmd[cmd.index("-avoid_negative_ts") + 1] == "make_zero"


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
async def test_move_to_recordings_is_idempotent_when_file_already_moved(tmp_path):
    from services.recorder import _move_to_recordings

    temp_path = tmp_path / "temp" / "cam1" / "2026-05-14" / "2026-05-15_00-00.mp4"
    final_path = tmp_path / "recordings" / "cam1" / "2026-05-15" / "2026-05-15_00-00.mp4"
    final_path.parent.mkdir(parents=True)
    final_path.write_bytes(b"already moved")

    assert await _move_to_recordings(str(temp_path), "cam1", str(tmp_path / "recordings")) is True
    assert final_path.read_bytes() == b"already moved"


@pytest.mark.asyncio
async def test_db_execute_retries_when_database_is_locked(monkeypatch, tmp_path):
    from services import recorder

    attempts = 0

    class FakeResult:
        rowcount = 1

    class FakeDb:
        async def __aenter__(self):
            return self

        async def __aexit__(self, exc_type, exc, tb):
            return False

        async def execute(self, sql, params=()):
            nonlocal attempts
            if sql.startswith("PRAGMA"):
                return FakeResult()
            attempts += 1
            if attempts == 1:
                import sqlite3
                raise sqlite3.OperationalError("database is locked")
            return FakeResult()

        async def commit(self):
            pass

    monkeypatch.setattr(recorder.aiosqlite, "connect", lambda *args, **kwargs: FakeDb())

    result = await recorder._execute_db_write_with_retry(
        str(tmp_path / "test.db"),
        [("UPDATE recordings SET ts_end=?", (1.0,))],
        retries=2,
        base_delay=0,
    )

    assert result == [1]
    assert attempts == 2


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


@pytest.mark.asyncio
async def test_midnight_restart_starts_recorder_even_when_stop_hangs(monkeypatch):
    from services import recorder

    calls = []
    original_processes = dict(recorder._processes)
    original_active_procs = dict(recorder._active_procs)
    original_stopping = set(recorder._stopping_rec)
    original_sender = recorder._event_alert_sender
    try:
        recorder._processes["camera-yard"] = object()
        recorder._active_procs["camera-yard"] = None
        recorder._stopping_rec.discard("camera-yard")

        async def hanging_stop(cam_id):
            calls.append(("stop", cam_id))
            await recorder.asyncio.sleep(10)

        async def fake_start(cam_id, segment_minutes, recordings_dir, temp_dir):
            calls.append(("start", cam_id, segment_minutes, recordings_dir, temp_dir))
            recorder._processes[cam_id] = "restarted"

        monkeypatch.setattr(recorder, "stop_recording", hanging_stop)
        monkeypatch.setattr(recorder, "start_recording", fake_start)
        recorder.set_event_alert_sender(None)

        await recorder._restart_recording_for_new_day(
            "camera-yard",
            segment_minutes=30,
            recordings_dir="/recordings",
            temp_dir="/temp",
            stop_timeout=0.01,
        )

        assert calls == [
            ("stop", "camera-yard"),
            ("start", "camera-yard", 30, "/recordings", "/temp"),
        ]
        assert recorder._processes["camera-yard"] == "restarted"
        assert "camera-yard" not in recorder._stopping_rec
    finally:
        recorder._processes.clear()
        recorder._processes.update(original_processes)
        recorder._active_procs.clear()
        recorder._active_procs.update(original_active_procs)
        recorder._stopping_rec.clear()
        recorder._stopping_rec.update(original_stopping)
        recorder.set_event_alert_sender(original_sender)


@pytest.mark.asyncio
async def test_recording_loop_exits_without_retry_when_process_is_superseded(monkeypatch):
    from services import recorder

    events = []
    original_active_procs = dict(recorder._active_procs)
    original_processes = dict(recorder._processes)
    original_stderr_tasks = dict(recorder._stderr_tasks)
    original_segments = dict(recorder._current_segment_paths)
    original_sender = recorder._event_alert_sender

    class FakeProc:
        pid = 111
        returncode = 255

        async def wait(self):
            newer_proc = FakeProc()
            newer_task = recorder.asyncio.create_task(recorder.asyncio.sleep(60))
            recorder._active_procs["cam1"] = newer_proc  # type: ignore[assignment]
            recorder._stderr_tasks["cam1"] = newer_task
            recorder._current_segment_paths["cam1"] = "/tmp/newer.mp4"
            return self.returncode

    async def fake_subprocess_exec(*args, **kwargs):
        return FakeProc()

    async def fake_watch_stderr(*args, **kwargs):
        await recorder.asyncio.sleep(60)

    async def fake_get_setting(name):
        return "30"

    async def fake_send_event(*args, **kwargs):
        events.append((args, kwargs))

    try:
        recorder._active_procs.clear()
        recorder._processes.clear()
        recorder._stderr_tasks.clear()
        recorder._current_segment_paths.clear()
        recorder.set_event_alert_sender(None)
        monkeypatch.setattr(recorder.asyncio, "create_subprocess_exec", fake_subprocess_exec)
        monkeypatch.setattr(recorder, "_watch_stderr", fake_watch_stderr)
        monkeypatch.setattr(recorder, "get_setting", fake_get_setting)
        monkeypatch.setattr(recorder, "_send_recording_event_alert", fake_send_event)

        await recorder.asyncio.wait_for(
            recorder._run_recording("cam1", 30, "/recordings", "/tmp", "/tmp/test.db"),
            timeout=1,
        )

        assert events == []
        assert recorder._current_segment_paths["cam1"] == "/tmp/newer.mp4"
        assert recorder._active_procs["cam1"] is not None
        assert "cam1" in recorder._stderr_tasks
    finally:
        recorder._active_procs.clear()
        recorder._active_procs.update(original_active_procs)
        recorder._processes.clear()
        recorder._processes.update(original_processes)
        recorder._stderr_tasks.clear()
        recorder._stderr_tasks.update(original_stderr_tasks)
        recorder._current_segment_paths.clear()
        recorder._current_segment_paths.update(original_segments)
        recorder.set_event_alert_sender(original_sender)


@pytest.mark.asyncio
async def test_recording_event_alert_sender_emits_named_event():
    from services import recorder

    sent = []
    original_processes = dict(recorder._processes)
    original_sender = recorder._event_alert_sender

    class FakeSender:
        async def send_recording_health_report(self, report, *, event="recording_health_failed"):
            sent.append((event, report))
            return True

    try:
        recorder._processes.clear()
        recorder._processes["cam1"] = object()
        recorder.set_event_alert_sender(FakeSender())
        await recorder._send_recording_event_alert(
            "recording_process_failed",
            camera_id="cam1",
            message="ffmpeg exited",
        )
    finally:
        recorder._processes.clear()
        recorder._processes.update(original_processes)
        recorder.set_event_alert_sender(original_sender)

    assert len(sent) == 1
    event, report = sent[0]
    assert event == "recording_process_failed"
    assert report.issues[0].code == "recording_process_failed"
    assert report.issues[0].camera_id == "cam1"


def test_recording_process_exit_alert_only_for_repeated_fast_failures():
    from services import recorder

    assert recorder._should_alert_recording_process_exit(ran=1800, retry_delay=5) is False
    assert recorder._should_alert_recording_process_exit(ran=10, retry_delay=10) is False
    assert recorder._should_alert_recording_process_exit(ran=10, retry_delay=20) is False
    assert recorder._should_alert_recording_process_exit(ran=10, retry_delay=30) is True
