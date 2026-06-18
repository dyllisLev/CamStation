from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path


@dataclass(slots=True)
class CameraConfig:
    id: str
    name: str
    enabled: bool
    has_sub: bool


_ENABLED_STREAM_RE = re.compile(r"^(?P<indent>\s{2,})(?P<name>[^#\s][^:]*):(?P<value>.*)$")
_DISABLED_STREAM_RE = re.compile(r"^#\s*(?P<indent>\s{2,})(?P<name>[^#\s][^:]*):(?P<value>.*)$")


def _stream_bounds(lines: list[str]) -> tuple[int, int]:
    start = None
    for i, line in enumerate(lines):
        if line.strip() == "streams:":
            start = i + 1
            break
    if start is None:
        raise ValueError("go2rtc config has no streams section")
    end = len(lines)
    for i in range(start, len(lines)):
        line = lines[i]
        if line and not line.startswith((" ", "#", "\t")):
            end = i
            break
    return start, end


def _match_stream_line(line: str):
    match = _ENABLED_STREAM_RE.match(line)
    if match:
        # Skip YAML list items (e.g. "    - rtsp://..." or "    - on_demand: false")
        # which are go2rtc source entries, not stream definitions.
        if match.group("name").strip().startswith("-"):
            return None, False
        return match, True
    match = _DISABLED_STREAM_RE.match(line)
    if match:
        if match.group("name").strip().startswith("-"):
            return None, False
        return match, False
    return None, False


def _base_name(name: str) -> str:
    return name[:-4] if name.endswith("_sub") else name


def list_camera_configs(config_path: str) -> list[CameraConfig]:
    path = Path(config_path)
    lines = path.read_text(encoding="utf-8").splitlines()
    start, end = _stream_bounds(lines)
    order: list[str] = []
    states: dict[str, dict[str, bool]] = {}

    for line in lines[start:end]:
        match, enabled = _match_stream_line(line)
        if not match:
            continue
        name = match.group("name").strip()
        # Skip YAML list items (e.g. "- rtsp://...", "- on_demand: false",
        # "- ffmpeg:...") that the regex can misparse as stream names.
        if name.startswith("-"):
            continue
        base = _base_name(name)
        if base not in states:
            order.append(base)
            states[base] = {"main": False, "sub": False, "enabled": False}
        if name.endswith("_sub"):
            states[base]["sub"] = True
        else:
            states[base]["main"] = True
            states[base]["enabled"] = enabled

    return [
        CameraConfig(id=cam_id, name=cam_id, enabled=states[cam_id]["enabled"], has_sub=states[cam_id]["sub"])
        for cam_id in order
        if states[cam_id]["main"]
    ]


def enabled_camera_ids(config_path: str) -> list[str]:
    return [camera.id for camera in list_camera_configs(config_path) if camera.enabled]


def _is_target_stream_line(line: str, camera_id: str) -> bool:
    match, _enabled = _match_stream_line(line)
    if not match:
        return False
    name = match.group("name").strip()
    return name == camera_id or name == f"{camera_id}_sub"


def _disable_line(line: str) -> str:
    if line.lstrip().startswith("#"):
        return line
    return "# " + line


def _enable_line(line: str) -> str:
    if not line.startswith("#"):
        return line
    return re.sub(r"^#\s?", "", line, count=1)


def set_camera_enabled(config_path: str, camera_id: str, enabled: bool) -> bool:
    path = Path(config_path)
    original = path.read_text(encoding="utf-8")
    lines = original.splitlines()
    has_final_newline = original.endswith("\n")
    start, end = _stream_bounds(lines)

    found = False
    changed = False
    output: list[str] = []
    i = 0
    stamp = datetime.now().strftime("%Y-%m-%d")

    while i < len(lines):
        line = lines[i]
        in_streams = start <= i < end
        if in_streams and enabled and line.startswith("# disabled") and i + 1 < end and _is_target_stream_line(lines[i + 1], camera_id):
            changed = True
            i += 1
            continue

        if in_streams and _is_target_stream_line(line, camera_id):
            found = True
            new_line = _enable_line(line) if enabled else _disable_line(line)
            if new_line != line:
                changed = True
                if not enabled:
                    output.append(f"# disabled {stamp}: 설정 화면에서 비활성화")
                output.append(new_line)
            else:
                output.append(line)
        else:
            output.append(line)
        i += 1

    if not found:
        raise KeyError(camera_id)
    if changed:
        path.write_text("\n".join(output) + ("\n" if has_final_newline else ""), encoding="utf-8")
    return changed
