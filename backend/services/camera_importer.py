from __future__ import annotations

import re
import time
from dataclasses import dataclass
from pathlib import Path

import aiosqlite

from config import get_db_path
from services.camera_config import list_camera_configs, _match_stream_line, _stream_bounds


@dataclass(slots=True)
class CameraImportResult:
    created: int = 0
    skipped: int = 0


_RTSP_IN_LINE_RE = re.compile(r"rtsp://\S+")


def _extract_stream_urls(config_path: str) -> dict[str, str]:
    """Extract stream URLs keyed by camera id from go2rtc config.

    Handles both single-line format (``cam: rtsp://...``) and YAML list
    format (``cam:\\n  - rtsp://...``).
    """
    text = Path(config_path).read_text(encoding="utf-8")
    lines = text.splitlines()
    start, end = _stream_bounds(lines)
    urls: dict[str, str] = {}
    current_name: str | None = None

    for line in lines[start:end]:
        match, _enabled = _match_stream_line(line)
        if match:
            name = match.group("name").strip()
            value = match.group("value").strip()
            current_name = name
            if value.startswith("|") or value == "" or value.startswith("["):
                continue
            urls[name] = value.strip().strip('"').strip("'")
        else:
            stripped = line.lstrip()
            # YAML list items (e.g. "    - rtsp://..." or "    - on_demand: false")
            # are children of the current stream and should not reset context.
            if current_name and stripped.startswith("- "):
                item_value = stripped[2:].strip()
                if item_value.startswith(("rtsp://", "ffmpeg:")):
                    urls.setdefault(current_name, item_value)
                continue
            # Non-list, non-comment line at shallower indent resets context.
            if stripped and not stripped.startswith("#"):
                current_name = None
    return urls


async def import_cameras_from_go2rtc_config(config_path: str, *, db_path: str | None = None) -> CameraImportResult:
    cameras = list_camera_configs(config_path)
    urls = _extract_stream_urls(config_path)
    path = db_path or get_db_path()
    now = time.time()
    result = CameraImportResult()

    async with aiosqlite.connect(path) as db:
        for index, camera in enumerate(cameras):
            existing = await db.execute_fetchall("SELECT 1 FROM cameras WHERE id=?", (camera.id,))
            if existing:
                result.skipped += 1
                continue
            main_url = urls.get(camera.id)
            if not main_url:
                result.skipped += 1
                continue
            await db.execute(
                """
                INSERT INTO cameras(
                    id, display_name, enabled, main_stream_url, sub_stream_url,
                    sort_order, created_at, updated_at
                ) VALUES(?,?,?,?,?,?,?,?)
                """,
                (
                    camera.id,
                    camera.name,
                    1 if camera.enabled else 0,
                    main_url,
                    urls.get(f"{camera.id}_sub"),
                    index,
                    now,
                    now,
                ),
            )
            result.created += 1
        await db.commit()
    return result
