from __future__ import annotations

import time
from typing import Any

import aiosqlite

from config import GO2RTC_CONFIG, get_db_path
from models import CameraAdminItem, CameraConfigStatus, CameraCreateRequest, CameraUpdateRequest
from services.camera_config import enabled_camera_ids as config_enabled_camera_ids
from services.camera_config import list_camera_configs


def _is_stream_online(streams: dict[str, Any], camera_id: str) -> bool:
    info = streams.get(camera_id) or {}
    producers = info.get("producers") or []
    return any("id" in producer for producer in producers)


async def _camera_table_exists(db: aiosqlite.Connection) -> bool:
    rows = await db.execute_fetchall(
        "SELECT name FROM sqlite_master WHERE type='table' AND name='cameras'"
    )
    return bool(rows)


def _row_to_admin_item(row: aiosqlite.Row, stream_map: dict[str, Any] | None = None) -> CameraAdminItem:
    streams = stream_map or {}
    camera_id = row["id"]
    has_sub = bool(row["sub_stream_url"]) or f"{camera_id}_sub" in streams
    return CameraAdminItem(
        id=camera_id,
        display_name=row["display_name"],
        location=row["location"],
        enabled=bool(row["enabled"]),
        archived=row["archived_at"] is not None,
        online=bool(row["enabled"]) and _is_stream_online(streams, camera_id),
        has_sub=has_sub,
        main_stream_configured=bool(row["main_stream_url"]),
        sub_stream_configured=bool(row["sub_stream_url"]),
        onvif_configured=bool(row["onvif_host"] and row["onvif_port"]),
        main_stream_url=row["main_stream_url"],
        sub_stream_url=row["sub_stream_url"],
        onvif_host=row["onvif_host"],
        onvif_port=row["onvif_port"],
        onvif_username=row["onvif_username"],
        onvif_password=row["onvif_password"],
        sort_order=int(row["sort_order"] or 0),
        notes=row["notes"],
    )


async def _fetch_camera_item(
    db: aiosqlite.Connection,
    camera_id: str,
    *,
    stream_map: dict[str, Any] | None = None,
) -> CameraAdminItem | None:
    db.row_factory = aiosqlite.Row
    rows = await db.execute_fetchall(
        """
        SELECT id, display_name, location, enabled, archived_at, main_stream_url,
               sub_stream_url, onvif_host, onvif_port, onvif_username, onvif_password,
               sort_order, notes
        FROM cameras
        WHERE id=?
        """,
        (camera_id,),
    )
    if not rows:
        return None
    return _row_to_admin_item(rows[0], stream_map)


async def list_camera_admin_items(
    *,
    db_path: str | None = None,
    include_archived: bool = False,
    streams: dict[str, Any] | None = None,
) -> list[CameraAdminItem]:
    path = db_path or get_db_path()
    stream_map = streams or {}
    async with aiosqlite.connect(path) as db:
        db.row_factory = aiosqlite.Row
        if not await _camera_table_exists(db):
            return []
        where = ""
        if not include_archived:
            where = "WHERE archived_at IS NULL"
        rows = await db.execute_fetchall(
            f"""
            SELECT id, display_name, location, enabled, archived_at, main_stream_url,
                   sub_stream_url, onvif_host, onvif_port, onvif_username, onvif_password,
                   sort_order, notes
            FROM cameras
            {where}
            ORDER BY sort_order, id
            """
        )
    result: list[CameraAdminItem] = []
    for row in rows:
        result.append(_row_to_admin_item(row, stream_map))
    return result


async def create_camera(payload: CameraCreateRequest, *, db_path: str | None = None) -> CameraAdminItem:
    path = db_path or get_db_path()
    now = time.time()
    async with aiosqlite.connect(path) as db:
        db.row_factory = aiosqlite.Row
        existing = await db.execute_fetchall("SELECT 1 FROM cameras WHERE id=?", (payload.id,))
        if existing:
            raise ValueError("camera already exists")
        await db.execute(
            """
            INSERT INTO cameras(
                id, display_name, location, enabled, archived_at,
                main_stream_url, sub_stream_url, onvif_host, onvif_port,
                onvif_username, onvif_password, sort_order, notes, created_at, updated_at
            ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
            """,
            (
                payload.id,
                payload.display_name,
                payload.location,
                1 if payload.enabled else 0,
                None,
                payload.main_stream_url,
                payload.sub_stream_url,
                payload.onvif_host,
                payload.onvif_port,
                payload.onvif_username,
                payload.onvif_password,
                payload.sort_order,
                payload.notes,
                now,
                now,
            ),
        )
        await db.commit()
        item = await _fetch_camera_item(db, payload.id)
    if item is None:
        raise KeyError(payload.id)
    return item


async def update_camera(camera_id: str, payload: CameraUpdateRequest, *, db_path: str | None = None) -> CameraAdminItem:
    path = db_path or get_db_path()
    data = payload.model_dump(exclude_unset=True)
    data.pop("id", None)
    if not data:
        async with aiosqlite.connect(path) as db:
            item = await _fetch_camera_item(db, camera_id)
        if item is None:
            raise KeyError(camera_id)
        return item

    allowed = {
        "display_name", "location", "enabled", "main_stream_url", "sub_stream_url",
        "onvif_host", "onvif_port", "onvif_username", "onvif_password", "sort_order", "notes",
    }
    assignments = []
    values = []
    for key, value in data.items():
        if key not in allowed:
            continue
        assignments.append(f"{key}=?")
        values.append(1 if key == "enabled" and value is True else 0 if key == "enabled" and value is False else value)
    assignments.append("updated_at=?")
    values.append(time.time())
    values.append(camera_id)

    async with aiosqlite.connect(path) as db:
        db.row_factory = aiosqlite.Row
        cursor = await db.execute(
            f"UPDATE cameras SET {', '.join(assignments)} WHERE id=?",
            tuple(values),
        )
        if cursor.rowcount == 0:
            raise KeyError(camera_id)
        await db.commit()
        item = await _fetch_camera_item(db, camera_id)
    if item is None:
        raise KeyError(camera_id)
    return item


async def set_camera_enabled(camera_id: str, enabled: bool, *, db_path: str | None = None) -> CameraAdminItem:
    return await update_camera(camera_id, CameraUpdateRequest(enabled=enabled), db_path=db_path)


async def archive_camera(camera_id: str, *, db_path: str | None = None) -> CameraAdminItem:
    path = db_path or get_db_path()
    now = time.time()
    async with aiosqlite.connect(path) as db:
        db.row_factory = aiosqlite.Row
        cursor = await db.execute(
            "UPDATE cameras SET enabled=0, archived_at=?, updated_at=? WHERE id=?",
            (now, now, camera_id),
        )
        if cursor.rowcount == 0:
            raise KeyError(camera_id)
        await db.commit()
        item = await _fetch_camera_item(db, camera_id)
    if item is None:
        raise KeyError(camera_id)
    return item


async def get_enabled_camera_ids(*, db_path: str | None = None) -> list[str]:
    path = db_path or get_db_path()
    try:
        async with aiosqlite.connect(path) as db:
            if not await _camera_table_exists(db):
                return config_enabled_camera_ids(GO2RTC_CONFIG)
            total_rows = await db.execute_fetchall("SELECT COUNT(*) FROM cameras")
            if total_rows and total_rows[0][0] > 0:
                rows = await db.execute_fetchall(
                    """
                    SELECT id FROM cameras
                    WHERE enabled=1 AND archived_at IS NULL
                    ORDER BY sort_order, id
                    """
                )
                return [row[0] for row in rows]
    except Exception:
        return config_enabled_camera_ids(GO2RTC_CONFIG)
    return config_enabled_camera_ids(GO2RTC_CONFIG)


async def get_enabled_sub_camera_ids(*, db_path: str | None = None) -> list[str]:
    path = db_path or get_db_path()
    try:
        async with aiosqlite.connect(path) as db:
            if not await _camera_table_exists(db):
                return []
            total_rows = await db.execute_fetchall("SELECT COUNT(*) FROM cameras")
            if total_rows and total_rows[0][0] > 0:
                rows = await db.execute_fetchall(
                    """
                    SELECT id FROM cameras
                    WHERE enabled=1 AND archived_at IS NULL
                      AND sub_stream_url IS NOT NULL AND sub_stream_url != ''
                    ORDER BY sort_order, id
                    """
                )
                return [row[0] for row in rows]
    except Exception:
        return []
    return []


async def list_legacy_camera_config_status(
    *,
    streams: dict[str, Any] | None = None,
) -> list[CameraConfigStatus]:
    admin_items = await list_camera_admin_items(streams=streams)
    if admin_items:
        return [
            CameraConfigStatus(
                id=item.id,
                name=item.display_name,
                enabled=item.enabled,
                online=item.online,
                has_sub=item.has_sub,
            )
            for item in admin_items
        ]

    stream_map = streams or {}
    return [
        CameraConfigStatus(
            id=cfg.id,
            name=cfg.name,
            enabled=cfg.enabled,
            online=cfg.enabled and _is_stream_online(stream_map, cfg.id),
            has_sub=cfg.has_sub,
        )
        for cfg in list_camera_configs(GO2RTC_CONFIG)
    ]
