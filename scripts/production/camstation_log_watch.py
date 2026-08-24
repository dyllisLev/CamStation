#!/usr/bin/env python3
"""Emit one bounded, non-sensitive CamStation production health snapshot."""

from __future__ import annotations

import argparse
import datetime as dt
import fcntl
import json
import math
import os
import re
import sqlite3
import stat
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable


UTC = dt.timezone.utc
CONTAINER_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
IMAGE_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_./:@+-]{0,191}$")
ALERT_RANK = {"degraded": 1, "error": 2}
MAX_HTTP_BYTES = 8 * 1024 * 1024
MAX_RECORD_BYTES = 16 * 1024


class WatchConfigError(ValueError):
    pass


class ProbeError(RuntimeError):
    pass


def required_env(environ: dict[str, str], name: str) -> str:
    value = environ.get(name, "").strip()
    if not value:
        raise WatchConfigError(f"missing_{name.lower()}")
    return value


def int_env(environ: dict[str, str], name: str, default: int, minimum: int, maximum: int) -> int:
    raw = environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError as exc:
        raise WatchConfigError(f"invalid_{name.lower()}") from exc
    if value < minimum or value > maximum:
        raise WatchConfigError(f"invalid_{name.lower()}")
    return value


def absolute_path(value: str, name: str) -> Path:
    path = Path(value)
    if not path.is_absolute() or "\x00" in value:
        raise WatchConfigError(f"invalid_{name.lower()}")
    return path


def validate_api_base(value: str) -> str:
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or parsed.path not in {"", "/"}
    ):
        raise WatchConfigError("invalid_camstation_watch_api_base")
    return value.rstrip("/")


@dataclass(frozen=True)
class Config:
    api_base: str
    container: str
    docker_bin: Path
    db_path: Path
    daemon_log: Path
    output_log: Path
    state_path: Path
    media_path: Path
    lock_path: Path
    request_timeout_seconds: int = 5
    log_window_seconds: int = 300
    logger_stale_seconds: int = 180
    viewer_heartbeat_stale_seconds: int = 30
    viewer_progress_stale_seconds: int = 90
    recorder_segment_grace_seconds: int = 300
    warn_burst_threshold: int = 10
    disk_warn_percent: int = 90
    disk_error_percent: int = 95
    state_min_free_bytes: int = 2 * 1024 * 1024 * 1024
    media_min_free_bytes: int = 20 * 1024 * 1024 * 1024
    output_max_bytes: int = 10 * 1024 * 1024
    output_files: int = 4

    @classmethod
    def from_env(cls, environ: dict[str, str]) -> "Config":
        container = required_env(environ, "CAMSTATION_WATCH_CONTAINER")
        if not CONTAINER_RE.fullmatch(container):
            raise WatchConfigError("invalid_camstation_watch_container")
        return cls(
            api_base=validate_api_base(required_env(environ, "CAMSTATION_WATCH_API_BASE")),
            container=container,
            docker_bin=absolute_path(environ.get("CAMSTATION_WATCH_DOCKER_BIN", "/usr/bin/docker"), "docker_bin"),
            db_path=absolute_path(required_env(environ, "CAMSTATION_WATCH_DB_PATH"), "db_path"),
            daemon_log=absolute_path(required_env(environ, "CAMSTATION_WATCH_DAEMON_LOG"), "daemon_log"),
            output_log=absolute_path(required_env(environ, "CAMSTATION_WATCH_OUTPUT_LOG"), "output_log"),
            state_path=absolute_path(required_env(environ, "CAMSTATION_WATCH_STATE_PATH"), "state_path"),
            media_path=absolute_path(required_env(environ, "CAMSTATION_WATCH_MEDIA_PATH"), "media_path"),
            lock_path=absolute_path(environ.get("CAMSTATION_WATCH_LOCK_PATH", "/run/lock/camstation-log-watch.lock"), "lock_path"),
            request_timeout_seconds=int_env(environ, "CAMSTATION_WATCH_TIMEOUT_SECONDS", 5, 1, 30),
            log_window_seconds=int_env(environ, "CAMSTATION_WATCH_LOG_WINDOW_SECONDS", 300, 60, 3600),
            logger_stale_seconds=int_env(environ, "CAMSTATION_WATCH_LOGGER_STALE_SECONDS", 180, 30, 3600),
            viewer_heartbeat_stale_seconds=int_env(
                environ, "CAMSTATION_WATCH_VIEWER_HEARTBEAT_STALE_SECONDS", 30, 15, 600
            ),
            viewer_progress_stale_seconds=int_env(environ, "CAMSTATION_WATCH_VIEWER_PROGRESS_STALE_SECONDS", 90, 15, 600),
            recorder_segment_grace_seconds=int_env(environ, "CAMSTATION_WATCH_RECORDER_SEGMENT_GRACE_SECONDS", 300, 60, 3600),
            warn_burst_threshold=int_env(environ, "CAMSTATION_WATCH_WARN_BURST", 10, 1, 10000),
            disk_warn_percent=int_env(environ, "CAMSTATION_WATCH_DISK_WARN_PERCENT", 90, 1, 99),
            disk_error_percent=int_env(environ, "CAMSTATION_WATCH_DISK_ERROR_PERCENT", 95, 2, 100),
            state_min_free_bytes=int_env(environ, "CAMSTATION_WATCH_STATE_MIN_FREE_BYTES", 2 * 1024**3, 0, 2**63 - 1),
            media_min_free_bytes=int_env(environ, "CAMSTATION_WATCH_MEDIA_MIN_FREE_BYTES", 20 * 1024**3, 0, 2**63 - 1),
            output_max_bytes=int_env(environ, "CAMSTATION_WATCH_OUTPUT_MAX_MB", 10, 1, 1024) * 1024 * 1024,
            output_files=int_env(environ, "CAMSTATION_WATCH_OUTPUT_FILES", 4, 1, 64),
        ).validated()

    def validated(self) -> "Config":
        if self.disk_error_percent <= self.disk_warn_percent:
            raise WatchConfigError("disk_threshold_order_invalid")
        if self.daemon_log == self.output_log:
            raise WatchConfigError("log_paths_must_differ")
        if self.db_path in {self.daemon_log, self.output_log}:
            raise WatchConfigError("database_log_paths_must_differ")
        return self


def utc_now() -> dt.datetime:
    return dt.datetime.now(tz=UTC)


def parse_timestamp(value: Any) -> dt.datetime | None:
    if not isinstance(value, str) or not value or len(value) > 64:
        return None
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return None
    return parsed.astimezone(UTC)


def age_seconds(now: dt.datetime, value: dt.datetime | None) -> int | None:
    if value is None:
        return None
    return max(0, int((now - value).total_seconds()))


def http_get_json(base: str, endpoint: str, timeout: int) -> Any:
    request = urllib.request.Request(
        base + endpoint,
        headers={"Accept": "application/json", "User-Agent": "camstation-log-watch/1"},
        method="GET",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            if response.status != 200:
                raise ProbeError("http_status")
            payload = response.read(MAX_HTTP_BYTES + 1)
    except (OSError, TimeoutError) as exc:
        raise ProbeError("http_request") from exc
    if len(payload) > MAX_HTTP_BYTES:
        raise ProbeError("http_oversize")
    try:
        return json.loads(payload)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ProbeError("http_json") from exc


def run_command(args: list[str], timeout: int) -> str:
    try:
        completed = subprocess.run(
            args,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ProbeError("command_failed") from exc
    if completed.returncode != 0:
        raise ProbeError("command_exit")
    return completed.stdout


def docker_state(config: Config, runner: Callable[[list[str], int], str]) -> dict[str, Any]:
    template = (
        '{"running":{{json .State.Running}},"status":{{json .State.Status}},'
        '"health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}null{{end}},'
        '"restartCount":{{json .RestartCount}},"image":{{json .Config.Image}}}'
    )
    output = runner(
        [str(config.docker_bin), "inspect", "--format", template, config.container],
        config.request_timeout_seconds,
    )
    try:
        value = json.loads(output)
    except json.JSONDecodeError as exc:
        raise ProbeError("docker_json") from exc
    if not isinstance(value, dict):
        raise ProbeError("docker_shape")
    image = value.get("image")
    if not isinstance(image, str) or not IMAGE_RE.fullmatch(image):
        image = "unknown"
    return {
        "running": value.get("running") is True,
        "healthy": value.get("health") == "healthy",
        "status": value.get("status") if value.get("status") in {"created", "running", "paused", "restarting", "removing", "exited", "dead"} else "unknown",
        "restartCount": value.get("restartCount") if isinstance(value.get("restartCount"), int) and value.get("restartCount") >= 0 else None,
        "image": image,
    }


def tail_bytes(path: Path, maximum: int = 4 * 1024 * 1024) -> bytes:
    with path.open("rb") as handle:
        handle.seek(0, os.SEEK_END)
        size = handle.tell()
        handle.seek(max(0, size - maximum), os.SEEK_SET)
        data = handle.read(maximum)
    if size > maximum:
        newline = data.find(b"\n")
        data = data[newline + 1 :] if newline >= 0 else b""
    return data


def summarize_log(path: Path, now: dt.datetime, window_seconds: int) -> dict[str, Any]:
    result: dict[str, Any] = {
        "present": False,
        "newestAgeSeconds": None,
        "warnWindow": 0,
        "errorWindow": 0,
        "invalidTailLines": 0,
    }
    try:
        data = tail_bytes(path)
    except OSError:
        return result
    result["present"] = True
    newest: dt.datetime | None = None
    cutoff = now - dt.timedelta(seconds=window_seconds)
    for raw in data.splitlines():
        if not raw.strip():
            continue
        try:
            record = json.loads(raw)
        except (UnicodeDecodeError, json.JSONDecodeError):
            result["invalidTailLines"] += 1
            continue
        if not isinstance(record, dict):
            result["invalidTailLines"] += 1
            continue
        timestamp = parse_timestamp(record.get("timestamp"))
        if timestamp is not None and (newest is None or timestamp > newest):
            newest = timestamp
        if timestamp is None or timestamp < cutoff or timestamp > now + dt.timedelta(minutes=1):
            continue
        if record.get("level") == "warn":
            result["warnWindow"] += 1
        elif record.get("level") == "error":
            result["errorWindow"] += 1
    result["newestAgeSeconds"] = age_seconds(now, newest)
    return result


def persistent_failure_count(config: Config, runner: Callable[[list[str], int], str]) -> int:
    output = runner(
        [
            str(config.docker_bin),
            "logs",
            "--since",
            f"{config.log_window_seconds}s",
            "--tail",
            "5000",
            config.container,
        ],
        config.request_timeout_seconds,
    )
    count = 0
    for line in output.splitlines():
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(record, dict) and record.get("component") == "opslog" and record.get("event") == "persistent_write_failed":
            count += 1
    return count


def disk_summary(path: Path) -> dict[str, int | float]:
    values = os.statvfs(path)
    total = values.f_frsize * values.f_blocks
    free = values.f_frsize * values.f_bavail
    used_percent = round(((total - free) * 100.0 / total), 1) if total > 0 else 100.0
    return {"usedPercent": used_percent, "freeBytes": int(free)}


def add_alert(alerts: dict[str, int], code: str, severity: str) -> None:
    rank = ALERT_RANK[severity]
    alerts[code] = max(alerts.get(code, 0), rank)


def aggregate_cameras(value: Any) -> tuple[dict[str, int], list[set[str]]]:
    if not isinstance(value, list):
        raise ProbeError("cameras_shape")
    enabled = [camera for camera in value if isinstance(camera, dict) and camera.get("enabled") is True]
    candidates: list[set[str]] = []
    for camera in enabled:
        names = {
            item
            for item in (camera.get("liveStreamName"), camera.get("focusStreamName"))
            if isinstance(item, str) and item and len(item) <= 128
        }
        candidates.append(names)
    return (
        {
            "total": sum(1 for camera in value if isinstance(camera, dict)),
            "enabled": len(enabled),
            "streaming": sum(1 for camera in enabled if camera.get("state") == "streaming"),
        },
        candidates,
    )


def aggregate_streams(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ProbeError("streams_shape")
    return {
        "running": value.get("running") is True,
        "mediaReady": value.get("mediaReady") is True,
        "expected": value.get("expectedLiveStreams") if isinstance(value.get("expectedLiveStreams"), int) else None,
        "ready": value.get("readyLiveStreams") if isinstance(value.get("readyLiveStreams"), int) else None,
    }


def aggregate_recorders(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict) or not isinstance(value.get("workers"), list):
        raise ProbeError("recorders_shape")
    segment_minutes = value.get("segmentMinutes")
    if not isinstance(segment_minutes, int) or segment_minutes <= 0:
        raise ProbeError("recorders_segment_minutes")
    workers = [worker for worker in value["workers"] if isinstance(worker, dict)]
    return {
        "enabled": value.get("enabled") is True,
        "segmentMinutes": segment_minutes,
        "workers": len(workers),
        "running": sum(1 for worker in workers if worker.get("state") == "running"),
        "current": sum(1 for worker in workers if isinstance(worker.get("current"), str) and bool(worker.get("current"))),
        "errors": sum(1 for worker in workers if isinstance(worker.get("lastError"), str) and bool(worker.get("lastError"))),
    }


def unix_age_seconds(now: dt.datetime, value: Any) -> int | None:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        return None
    numeric = float(value)
    if not math.isfinite(numeric) or numeric < 0:
        return None
    return max(0, int(now.timestamp() - numeric))


def recording_freshness(
    path: Path,
    now: dt.datetime,
    segment_minutes: int,
    grace_seconds: int,
) -> dict[str, Any]:
    if segment_minutes <= 0 or grace_seconds < 0:
        raise ProbeError("recording_freshness_config")
    try:
        mode = path.stat().st_mode
    except OSError as exc:
        raise ProbeError("recording_database_missing") from exc
    if not stat.S_ISREG(mode):
        raise ProbeError("recording_database_not_regular")

    uri = "file:" + urllib.parse.quote(str(path), safe="/") + "?mode=ro"
    try:
        connection = sqlite3.connect(uri, uri=True, timeout=2)
        connection.execute("PRAGMA query_only=ON")
        rows = connection.execute(
            """
            WITH active AS (
                SELECT stream_name, COUNT(*) AS current_rows, MIN(ts_start) AS current_start
                  FROM recording_segments
                 WHERE status IN ('recording','finalizing')
                 GROUP BY stream_name
            )
            SELECT active.current_rows, active.current_start,
                   (SELECT MAX(segment.ts_end)
                      FROM recording_segments AS segment
                     WHERE segment.stream_name=active.stream_name
                       AND segment.status='ready' AND segment.ts_end IS NOT NULL) AS latest_ready
              FROM active
            """
        ).fetchall()
    except sqlite3.Error as exc:
        raise ProbeError("recording_database_query") from exc
    finally:
        if "connection" in locals():
            connection.close()

    stale_after = segment_minutes * 60 + grace_seconds
    current_ages = [unix_age_seconds(now, row[1]) for row in rows]
    ready_ages = [unix_age_seconds(now, row[2]) for row in rows if row[2] is not None]
    if any(value is None for value in current_ages) or any(value is None for value in ready_ages):
        raise ProbeError("recording_database_timestamp")
    current_values = [value for value in current_ages if value is not None]
    ready_values = [value for value in ready_ages if value is not None]
    missing_ready = sum(
        1
        for row, current_age in zip(rows, current_values)
        if row[2] is None and current_age > stale_after
    )
    return {
        "present": True,
        "segmentMinutes": segment_minutes,
        "staleAfterSeconds": stale_after,
        "streamsWithCurrent": len(rows),
        "currentRows": sum(int(row[0]) for row in rows),
        "staleCurrent": sum(1 for value in current_values if value > stale_after),
        "oldestCurrentAgeSeconds": max(current_values, default=None),
        "streamsWithReady": len(ready_values),
        "staleReady": sum(1 for value in ready_values if value > stale_after),
        "missingReadyPastThreshold": missing_ready,
        "oldestReadyAgeSeconds": max(ready_values, default=None),
    }


def aggregate_viewers(
    value: Any,
    candidates: list[set[str]],
    now: dt.datetime,
    progress_stale_seconds: int,
    heartbeat_stale_seconds: int,
) -> dict[str, Any]:
    if not isinstance(value, list):
        raise ProbeError("viewers_shape")
    viewers = [viewer for viewer in value if isinstance(viewer, dict)]
    online = [viewer for viewer in viewers if viewer.get("status") == "online"]
    healthy = 0
    viewer_heartbeat_fresh = 0
    renderer_heartbeat_fresh = 0
    recent_streams: set[str] = set()
    newest_progress: dt.datetime | None = None
    newest_viewer_heartbeat: dt.datetime | None = None
    newest_renderer_heartbeat: dt.datetime | None = None
    for viewer in online:
        agent = viewer.get("agent") if isinstance(viewer.get("agent"), dict) else {}
        control = viewer.get("control") if isinstance(viewer.get("control"), dict) else {}
        process = viewer.get("viewer") if isinstance(viewer.get("viewer"), dict) else {}
        renderer = viewer.get("renderer") if isinstance(viewer.get("renderer"), dict) else {}
        viewer_heartbeat = parse_timestamp(process.get("lastHeartbeatAt"))
        renderer_heartbeat = parse_timestamp(renderer.get("lastHeartbeatAt"))
        viewer_is_fresh = (
            viewer_heartbeat is not None
            and age_seconds(now, viewer_heartbeat) < heartbeat_stale_seconds
        )
        renderer_is_fresh = (
            renderer_heartbeat is not None
            and age_seconds(now, renderer_heartbeat) < heartbeat_stale_seconds
        )
        if viewer_is_fresh:
            viewer_heartbeat_fresh += 1
            if newest_viewer_heartbeat is None or viewer_heartbeat > newest_viewer_heartbeat:
                newest_viewer_heartbeat = viewer_heartbeat
        if renderer_is_fresh:
            renderer_heartbeat_fresh += 1
            if newest_renderer_heartbeat is None or renderer_heartbeat > newest_renderer_heartbeat:
                newest_renderer_heartbeat = renderer_heartbeat
        if (
            agent.get("state") == "online"
            and control.get("state") in {"online", "healthy"}
            and process.get("state") == "running"
            and renderer.get("state") == "ready"
            and viewer_is_fresh
            and renderer_is_fresh
        ):
            healthy += 1
        streams = viewer.get("streams") if isinstance(viewer.get("streams"), list) else []
        for stream in streams:
            if not isinstance(stream, dict) or stream.get("state") != "playing":
                continue
            progress = parse_timestamp(stream.get("lastProgressAt"))
            if progress is None or age_seconds(now, progress) > progress_stale_seconds:
                continue
            name = stream.get("streamName")
            if isinstance(name, str) and name and len(name) <= 128:
                recent_streams.add(name)
                if newest_progress is None or progress > newest_progress:
                    newest_progress = progress
    receiving = sum(1 for names in candidates if names and not names.isdisjoint(recent_streams))
    return {
        "total": len(viewers),
        "online": len(online),
        "healthy": healthy,
        "viewerHeartbeatFresh": viewer_heartbeat_fresh,
        "rendererHeartbeatFresh": renderer_heartbeat_fresh,
        "newestViewerHeartbeatAgeSeconds": age_seconds(now, newest_viewer_heartbeat),
        "newestRendererHeartbeatAgeSeconds": age_seconds(now, newest_renderer_heartbeat),
        "expectedCameras": len(candidates),
        "receivingCameras": receiving,
        "newestProgressAgeSeconds": age_seconds(now, newest_progress),
    }


def collect_snapshot(
    config: Config,
    *,
    now: dt.datetime | None = None,
    fetcher: Callable[[str, str, int], Any] = http_get_json,
    runner: Callable[[list[str], int], str] = run_command,
    disk_probe: Callable[[Path], dict[str, int | float]] = disk_summary,
    recording_probe: Callable[[Path, dt.datetime, int, int], dict[str, Any]] = recording_freshness,
) -> dict[str, Any]:
    started = time.monotonic()
    now = (now or utc_now()).astimezone(UTC)
    alerts: dict[str, int] = {}
    snapshot: dict[str, Any] = {
        "timestamp": now.isoformat(timespec="seconds").replace("+00:00", "Z"),
    }

    try:
        container = docker_state(config, runner)
        snapshot["container"] = container
        if not container["running"]:
            add_alert(alerts, "container_down", "error")
        elif not container["healthy"]:
            add_alert(alerts, "container_unhealthy", "error")
        if isinstance(container["restartCount"], int) and container["restartCount"] > 0:
            add_alert(alerts, "container_restarted", "degraded")
    except ProbeError:
        snapshot["container"] = None
        add_alert(alerts, "container_probe_failed", "error")

    endpoints = {
        "health": "/api/health",
        "cameras": "/api/cameras",
        "recorders": "/api/recorders/status",
        "streams": "/api/streams/status",
        "viewers": "/api/viewers",
    }
    payloads: dict[str, Any] = {}
    for name, endpoint in endpoints.items():
        try:
            payloads[name] = fetcher(config.api_base, endpoint, config.request_timeout_seconds)
        except (ProbeError, OSError, TimeoutError, ValueError):
            payloads[name] = None
            add_alert(alerts, f"api_{name}_failed", "error")

    health = payloads["health"]
    api_healthy = isinstance(health, dict) and health.get("ok") is True
    snapshot["api"] = {"healthy": api_healthy}
    if health is not None and not api_healthy:
        add_alert(alerts, "api_health_unhealthy", "error")

    camera_candidates: list[set[str]] = []
    if payloads["cameras"] is not None:
        try:
            cameras, camera_candidates = aggregate_cameras(payloads["cameras"])
            snapshot["cameras"] = cameras
            if cameras["enabled"] <= 0:
                add_alert(alerts, "no_enabled_cameras", "error")
            elif cameras["streaming"] < cameras["enabled"]:
                add_alert(alerts, "camera_streaming_shortfall", "degraded")
        except ProbeError:
            snapshot["cameras"] = None
            add_alert(alerts, "api_cameras_invalid", "error")
    else:
        snapshot["cameras"] = None

    if payloads["streams"] is not None:
        try:
            streams = aggregate_streams(payloads["streams"])
            snapshot["streams"] = streams
            if not streams["running"] or not streams["mediaReady"]:
                add_alert(alerts, "stream_service_unready", "error")
            if isinstance(streams["expected"], int) and isinstance(streams["ready"], int) and streams["ready"] < streams["expected"]:
                add_alert(alerts, "stream_media_shortfall", "degraded")
        except ProbeError:
            snapshot["streams"] = None
            add_alert(alerts, "api_streams_invalid", "error")
    else:
        snapshot["streams"] = None

    if payloads["recorders"] is not None:
        try:
            recorders = aggregate_recorders(payloads["recorders"])
            snapshot["recorders"] = recorders
            expected = snapshot["cameras"]["enabled"] if isinstance(snapshot.get("cameras"), dict) else recorders["workers"]
            if not recorders["enabled"]:
                add_alert(alerts, "recorder_disabled", "error")
            elif recorders["running"] < expected or recorders["current"] < expected:
                add_alert(alerts, "recorder_shortfall", "degraded")
            if recorders["errors"] > 0:
                add_alert(alerts, "recorder_errors", "degraded")
        except ProbeError:
            snapshot["recorders"] = None
            add_alert(alerts, "api_recorders_invalid", "error")
    else:
        snapshot["recorders"] = None

    if isinstance(snapshot.get("recorders"), dict):
        try:
            recording_segments = recording_probe(
                config.db_path,
                now,
                snapshot["recorders"]["segmentMinutes"],
                config.recorder_segment_grace_seconds,
            )
            snapshot["recordingSegments"] = recording_segments
            expected = snapshot["cameras"]["enabled"] if isinstance(snapshot.get("cameras"), dict) else snapshot["recorders"]["workers"]
            if recording_segments["streamsWithCurrent"] < expected:
                add_alert(alerts, "recorder_segment_shortfall", "degraded")
            if (
                recording_segments["staleCurrent"] > 0
                or recording_segments["staleReady"] > 0
                or recording_segments["missingReadyPastThreshold"] > 0
            ):
                add_alert(alerts, "recorder_segment_stale", "error")
        except (ProbeError, OSError, KeyError, TypeError, ValueError):
            snapshot["recordingSegments"] = None
            add_alert(alerts, "recorder_segment_probe_failed", "error")
    else:
        snapshot["recordingSegments"] = None

    if payloads["viewers"] is not None:
        try:
            viewers = aggregate_viewers(
                payloads["viewers"],
                camera_candidates,
                now,
                config.viewer_progress_stale_seconds,
                config.viewer_heartbeat_stale_seconds,
            )
            snapshot["viewers"] = viewers
            if viewers["online"] < 1:
                add_alert(alerts, "viewer_offline", "error")
            elif viewers["healthy"] < viewers["online"]:
                add_alert(alerts, "viewer_health_degraded", "degraded")
            if viewers["online"] > 0 and viewers["viewerHeartbeatFresh"] < viewers["online"]:
                add_alert(alerts, "viewer_heartbeat_stale", "error")
            if viewers["online"] > 0 and viewers["rendererHeartbeatFresh"] < viewers["online"]:
                add_alert(alerts, "viewer_renderer_stale", "error")
            if viewers["expectedCameras"] > 0 and viewers["receivingCameras"] < viewers["expectedCameras"]:
                add_alert(
                    alerts,
                    "viewer_media_missing",
                    "error" if viewers["receivingCameras"] == 0 else "degraded",
                )
        except ProbeError:
            snapshot["viewers"] = None
            add_alert(alerts, "api_viewers_invalid", "error")
    else:
        snapshot["viewers"] = None

    logs = summarize_log(config.daemon_log, now, config.log_window_seconds)
    try:
        logs["persistentWriteFailuresWindow"] = persistent_failure_count(config, runner)
    except ProbeError:
        logs["persistentWriteFailuresWindow"] = None
        add_alert(alerts, "docker_log_probe_failed", "degraded")
    snapshot["logs"] = logs
    if not logs["present"] or logs["newestAgeSeconds"] is None or logs["newestAgeSeconds"] > config.logger_stale_seconds:
        add_alert(alerts, "persistent_log_stale", "error")
    if logs["invalidTailLines"] > 0:
        add_alert(alerts, "persistent_log_invalid", "degraded")
    if logs["warnWindow"] >= config.warn_burst_threshold:
        add_alert(alerts, "daemon_warn_burst", "degraded")
    if logs["errorWindow"] > 0:
        add_alert(alerts, "daemon_recent_error", "degraded")
    if isinstance(logs["persistentWriteFailuresWindow"], int) and logs["persistentWriteFailuresWindow"] > 0:
        add_alert(alerts, "persistent_log_write_failed", "error")

    disks: dict[str, Any] = {}
    for name, path, minimum in (
        ("state", config.state_path, config.state_min_free_bytes),
        ("media", config.media_path, config.media_min_free_bytes),
    ):
        try:
            disk = disk_probe(path)
            disks[name] = disk
            if disk["usedPercent"] >= config.disk_error_percent:
                add_alert(alerts, f"{name}_disk_critical", "error")
            elif disk["usedPercent"] >= config.disk_warn_percent or disk["freeBytes"] < minimum:
                add_alert(alerts, f"{name}_disk_low", "degraded")
        except (OSError, KeyError, TypeError, ValueError):
            disks[name] = None
            add_alert(alerts, f"{name}_disk_probe_failed", "error")
    snapshot["disk"] = disks

    highest = max(alerts.values(), default=0)
    snapshot["status"] = "error" if highest >= 2 else "degraded" if highest == 1 else "ok"
    snapshot["alerts"] = sorted(alerts)
    snapshot["durationMs"] = max(0, int((time.monotonic() - started) * 1000))
    return snapshot


def encode_record(record: dict[str, Any]) -> bytes:
    encoded = (json.dumps(record, ensure_ascii=False, separators=(",", ":"), sort_keys=True) + "\n").encode("utf-8")
    if len(encoded) > MAX_RECORD_BYTES:
        raise ProbeError("record_oversize")
    return encoded


def ensure_regular_or_absent(path: Path) -> None:
    try:
        mode = path.lstat().st_mode
    except FileNotFoundError:
        return
    if not stat.S_ISREG(mode):
        raise ProbeError("output_not_regular")


def rotate(path: Path, files: int) -> None:
    if files == 1:
        try:
            path.unlink()
        except FileNotFoundError:
            pass
        return
    for index in range(files - 1, 0, -1):
        source = path if index == 1 else Path(f"{path}.{index - 1}")
        destination = Path(f"{path}.{index}")
        ensure_regular_or_absent(source)
        ensure_regular_or_absent(destination)
        if not source.exists():
            continue
        try:
            destination.unlink()
        except FileNotFoundError:
            pass
        os.replace(source, destination)


def append_rotated(path: Path, payload: bytes, maximum: int, files: int) -> None:
    path.parent.mkdir(mode=0o750, parents=True, exist_ok=True)
    ensure_regular_or_absent(path)
    current_size = path.stat().st_size if path.exists() else 0
    if current_size > 0 and current_size + len(payload) > maximum:
        rotate(path, files)
    flags = os.O_WRONLY | os.O_CREAT | os.O_APPEND
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor = os.open(path, flags, 0o640)
    try:
        mode = os.fstat(descriptor).st_mode
        if not stat.S_ISREG(mode):
            raise ProbeError("output_not_regular")
        os.write(descriptor, payload)
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def acquire_lock(path: Path) -> int | None:
    path.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    ensure_regular_or_absent(path)
    flags = os.O_RDWR | os.O_CREAT
    if hasattr(os, "O_NOFOLLOW"):
        flags |= os.O_NOFOLLOW
    descriptor = os.open(path, flags, 0o640)
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(descriptor)
        return None
    return descriptor


def run(environ: dict[str, str]) -> int:
    try:
        config = Config.from_env(environ)
    except WatchConfigError as exc:
        print(json.dumps({"status": "error", "alerts": [str(exc)]}, separators=(",", ":")), file=sys.stderr)
        return 2
    lock_descriptor = acquire_lock(config.lock_path)
    if lock_descriptor is None:
        print(json.dumps({"status": "skipped", "alerts": ["watch_overlap"]}, separators=(",", ":")))
        return 0
    try:
        record = collect_snapshot(config)
        payload = encode_record(record)
        try:
            append_rotated(config.output_log, payload, config.output_max_bytes, config.output_files)
        except (OSError, ProbeError):
            failed = dict(record)
            failed["status"] = "error"
            failed["alerts"] = sorted(set(record["alerts"]) | {"watch_output_failed"})
            print(encode_record(failed).decode("utf-8"), end="")
            return 1
        print(payload.decode("utf-8"), end="")
        return 0
    finally:
        os.close(lock_descriptor)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args()
    return run(dict(os.environ))


if __name__ == "__main__":
    raise SystemExit(main())
