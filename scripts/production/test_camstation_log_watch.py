#!/usr/bin/env python3

from __future__ import annotations

import datetime as dt
import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path


MODULE_PATH = Path(__file__).with_name("camstation_log_watch.py")
SPEC = importlib.util.spec_from_file_location("camstation_log_watch", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
watch = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = watch
SPEC.loader.exec_module(watch)


NOW = dt.datetime(2026, 8, 13, 8, 0, 0, tzinfo=dt.timezone.utc)


class WatchTests(unittest.TestCase):
    def config(self, root: Path) -> object:
        daemon = root / "camstationd.jsonl"
        output = root / "operational-watch.jsonl"
        state = root / "state"
        media = root / "media"
        state.mkdir()
        media.mkdir()
        daemon.write_text(
            json.dumps(
                {
                    "timestamp": "2026-08-13T07:59:55Z",
                    "level": "debug",
                    "component": "stream.live_warm",
                    "event": "media_progress",
                    "streamName": "secret-camera-live",
                },
                separators=(",", ":"),
            )
            + "\n",
            encoding="utf-8",
        )
        return watch.Config(
            api_base="http://127.0.0.1:18080",
            container="camstation2",
            docker_bin=Path("/usr/bin/docker"),
            daemon_log=daemon,
            output_log=output,
            state_path=state,
            media_path=media,
            lock_path=root / "watch.lock",
        )

    @staticmethod
    def healthy_payloads() -> dict[str, object]:
        progress = "2026-08-13T07:59:58Z"
        cameras = []
        workers = []
        streams = []
        for index in range(8):
            stable = f"camera-{index}"
            cameras.append(
                {
                    "name": f"private-name-{index}",
                    "host": f"10.20.30.{index}",
                    "url": f"rtsp://operator:password@10.20.30.{index}/live",
                    "enabled": True,
                    "state": "streaming",
                    "liveStreamName": stable + "-live",
                    "focusStreamName": stable + "-focus",
                }
            )
            workers.append({"state": "running", "current": "managed-segment", "lastError": ""})
            streams.append(
                {
                    "streamName": stable + "-live",
                    "state": "playing",
                    "transport": "webrtc",
                    "lastProgressAt": progress,
                }
            )
        return {
            "/api/health": {"ok": True},
            "/api/cameras": cameras,
            "/api/recorders/status": {"enabled": True, "workers": workers},
            "/api/streams/status": {
                "running": True,
                "mediaReady": True,
                "expectedLiveStreams": 8,
                "readyLiveStreams": 8,
            },
            "/api/viewers": [
                {
                    "id": "private-viewer-id",
                    "status": "online",
                    "agent": {"state": "online"},
                    "control": {"state": "online"},
                    "viewer": {"state": "running"},
                    "renderer": {"state": "ready"},
                    "streams": streams,
                }
            ],
        }

    def test_healthy_snapshot_is_aggregate_only(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            config = self.config(Path(directory))
            payloads = self.healthy_payloads()

            def fetcher(_base: str, endpoint: str, _timeout: int) -> object:
                return payloads[endpoint]

            def runner(args: list[str], _timeout: int) -> str:
                if "inspect" in args:
                    return json.dumps(
                        {
                            "running": True,
                            "status": "running",
                            "health": "healthy",
                            "restartCount": 0,
                            "image": "camstation:2.0.0-test",
                        }
                    )
                return ""

            record = watch.collect_snapshot(
                config,
                now=NOW,
                fetcher=fetcher,
                runner=runner,
                disk_probe=lambda _path: {"usedPercent": 25.0, "freeBytes": 100 * 1024**3},
            )
            self.assertEqual(record["status"], "ok")
            self.assertEqual(record["alerts"], [])
            self.assertEqual(record["cameras"], {"total": 8, "enabled": 8, "streaming": 8})
            self.assertEqual(record["viewers"]["receivingCameras"], 8)
            encoded = watch.encode_record(record).decode("utf-8")
            for forbidden in ("private-name", "10.20.30", "operator", "password", "camera-0-live", "private-viewer-id"):
                self.assertNotIn(forbidden, encoded)

    def test_shortfalls_and_log_failures_are_alerted_without_raw_messages(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = self.config(root)
            with config.daemon_log.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps({"timestamp": "2026-08-13T07:59:57Z", "level": "error", "message": "rtsp://user:secret@camera/path"}) + "\n")
            payloads = self.healthy_payloads()
            payloads["/api/streams/status"] = {
                "running": True,
                "mediaReady": False,
                "expectedLiveStreams": 8,
                "readyLiveStreams": 7,
            }
            payloads["/api/recorders/status"]["workers"][0]["current"] = ""
            payloads["/api/viewers"][0]["streams"] = payloads["/api/viewers"][0]["streams"][:7]

            def fetcher(_base: str, endpoint: str, _timeout: int) -> object:
                return payloads[endpoint]

            def runner(args: list[str], _timeout: int) -> str:
                if "inspect" in args:
                    return '{"running":true,"status":"running","health":"healthy","restartCount":1,"image":"camstation:test"}'
                return '{"timestamp":"2026-08-13T07:59:59Z","component":"opslog","event":"persistent_write_failed","message":"/private/path"}\n'

            record = watch.collect_snapshot(
                config,
                now=NOW,
                fetcher=fetcher,
                runner=runner,
                disk_probe=lambda _path: {"usedPercent": 25.0, "freeBytes": 100 * 1024**3},
            )
            self.assertEqual(record["status"], "error")
            self.assertEqual(record["viewers"]["receivingCameras"], 7)
            for code in (
                "container_restarted",
                "stream_service_unready",
                "stream_media_shortfall",
                "recorder_shortfall",
                "viewer_media_missing",
                "daemon_recent_error",
                "persistent_log_write_failed",
            ):
                self.assertIn(code, record["alerts"])
            encoded = watch.encode_record(record).decode("utf-8")
            self.assertNotIn("rtsp://", encoded)
            self.assertNotIn("/private/path", encoded)

    def test_rotating_append_keeps_exact_file_bound(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "watch.jsonl"
            for index in range(8):
                watch.append_rotated(path, (f'{{"index":{index}}}\n').encode(), 30, 3)
            files = sorted(item.name for item in path.parent.iterdir())
            self.assertEqual(files, ["watch.jsonl", "watch.jsonl.1", "watch.jsonl.2"])
            for item in path.parent.iterdir():
                self.assertTrue(item.is_file())
                for line in item.read_text(encoding="utf-8").splitlines():
                    json.loads(line)

    def test_config_rejects_credentials_and_same_log_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            environ = {
                "CAMSTATION_WATCH_API_BASE": "http://user:secret@127.0.0.1:18080",
                "CAMSTATION_WATCH_CONTAINER": "camstation2",
                "CAMSTATION_WATCH_DAEMON_LOG": str(root / "same.jsonl"),
                "CAMSTATION_WATCH_OUTPUT_LOG": str(root / "same.jsonl"),
                "CAMSTATION_WATCH_STATE_PATH": str(root),
                "CAMSTATION_WATCH_MEDIA_PATH": str(root),
            }
            with self.assertRaises(watch.WatchConfigError):
                watch.Config.from_env(environ)


if __name__ == "__main__":
    unittest.main()
