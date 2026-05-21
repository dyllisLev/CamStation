from __future__ import annotations

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


def _extract_stream_urls(config_path: str) -> dict[str, str]:
    text = Path(config_path).read_text(encoding="utf-8")
    lines = text.splitlines()
    start, end = _stream_bounds(lines)
    urls: dict[str, str] = {}
    for line in lines[start:end]:
        match, _enabled = _match_stream_line(line)
        if not match:
            continue
        name = match.group("name").strip()
        value = match.group("value").strip()
        if value.startswith("|") or value == "":
            continue
        if value.startswith("["):
            continue
        urls[name] = value.strip().strip('"').strip("'")
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
