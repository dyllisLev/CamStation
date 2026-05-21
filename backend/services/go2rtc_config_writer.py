from __future__ import annotations

import time
from dataclasses import dataclass
from pathlib import Path

import aiosqlite

from config import get_db_path
from services.camera_config import _stream_bounds


@dataclass(slots=True)
class Go2RTCConfigWriteResult:
    changed: bool
    backup_path: str | None = None


async def _enabled_stream_rows(db_path: str) -> list[aiosqlite.Row]:
    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        return await db.execute_fetchall(
            """
            SELECT id, main_stream_url, sub_stream_url
            FROM cameras
            WHERE enabled=1 AND archived_at IS NULL
            ORDER BY sort_order, id
            """
        )


def _render_streams_section(rows: list[aiosqlite.Row]) -> list[str]:
    rendered = ["streams:"]
    for row in rows:
        camera_id = row["id"]
        rendered.append(f"  {camera_id}: {row['main_stream_url']}")
        if row["sub_stream_url"]:
            rendered.append(f"  {camera_id}_sub: {row['sub_stream_url']}")
    return rendered


def _replace_streams_section(existing_text: str, stream_lines: list[str]) -> str:
    had_final_newline = existing_text.endswith("\n")
    lines = existing_text.splitlines()
    if not lines:
        output = stream_lines
    else:
        try:
            start, end = _stream_bounds(lines)
            streams_header = start - 1
            output = lines[:streams_header] + stream_lines + lines[end:]
        except ValueError:
            output = lines + ([""] if lines[-1].strip() else []) + stream_lines
    text = "\n".join(output)
    if had_final_newline or not existing_text:
        text += "\n"
    return text


async def render_go2rtc_config_from_db(config_path: str, *, db_path: str | None = None) -> str:
    path = Path(config_path)
    existing = path.read_text(encoding="utf-8") if path.exists() else ""
    rows = await _enabled_stream_rows(db_path or get_db_path())
    return _replace_streams_section(existing, _render_streams_section(rows))


async def write_go2rtc_config_from_db(config_path: str, *, db_path: str | None = None) -> Go2RTCConfigWriteResult:
    path = Path(config_path)
    old_text = path.read_text(encoding="utf-8") if path.exists() else ""
    new_text = await render_go2rtc_config_from_db(config_path, db_path=db_path)
    if old_text == new_text:
        return Go2RTCConfigWriteResult(changed=False)

    path.parent.mkdir(parents=True, exist_ok=True)
    backup_path = None
    if path.exists():
        backup = path.with_name(f"{path.name}.bak.{int(time.time())}")
        backup.write_text(old_text, encoding="utf-8")
        backup_path = str(backup)

    tmp = path.with_name(f".{path.name}.tmp")
    tmp.write_text(new_text, encoding="utf-8")
    tmp.replace(path)
    return Go2RTCConfigWriteResult(changed=True, backup_path=backup_path)
