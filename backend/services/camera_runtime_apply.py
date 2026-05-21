from __future__ import annotations

import asyncio
import logging
import time

import aiosqlite

from config import RECORDINGS_DIR, TEMP_DIR, get_db_path
from database import get_setting
from services import recorder

logger = logging.getLogger(__name__)


async def restart_go2rtc() -> None:
    """Restart go2rtc after writing a new config.

    This runs inside the backend process on the operating server. Tests monkeypatch
    this function so local development never invokes systemctl.
    """
    proc = await asyncio.create_subprocess_exec(
        "systemctl",
        "restart",
        "go2rtc",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    stdout, stderr = await proc.communicate()
    if proc.returncode != 0:
        raise RuntimeError(
            f"go2rtc restart failed rc={proc.returncode} stdout={stdout.decode(errors='replace')} stderr={stderr.decode(errors='replace')}"
        )
    logger.info("go2rtc restarted after camera registry apply")


async def reconcile_recorders(camera_ids: list[str], sub_camera_ids: list[str]) -> None:
    """Bring in-process recorder tasks in line with enabled camera IDs."""
    desired = set(camera_ids)
    active = set(recorder.get_active())

    segment_minutes = int(await get_setting("segment_minutes") or "10")
    for cam_id in sorted(active - desired):
        await recorder.stop_recording(cam_id)
    for cam_id in camera_ids:
        if cam_id not in active:
            await recorder.start_recording(cam_id, segment_minutes, RECORDINGS_DIR, TEMP_DIR)

    active_sub = set(getattr(recorder, "_sub_processes", {}).keys())
    desired_sub = set(sub_camera_ids)
    for cam_id in sorted(active_sub - desired_sub):
        await recorder.stop_sub_keepalive(cam_id)
    for cam_id in sub_camera_ids:
        if cam_id not in active_sub:
            await recorder.start_sub_keepalive(cam_id)

    logger.info(
        "camera recorder reconciliation complete desired=%s sub_desired=%s active_before=%s sub_active_before=%s",
        sorted(desired),
        sorted(desired_sub),
        sorted(active),
        sorted(active_sub),
    )


async def enqueue_viewer_reload_commands(reason: str = "camera registry applied") -> int:
    now = time.time()
    async with aiosqlite.connect(get_db_path()) as db:
        rows = list(await db.execute_fetchall("SELECT client_id FROM viewer_clients ORDER BY last_seen DESC"))
        for (client_id,) in rows:
            await db.execute(
                """
                INSERT INTO viewer_commands(client_id, command, status, reason, created_at)
                VALUES(?,?,?,?,?)
                """,
                (client_id, "reload_page", "pending", reason, now),
            )
        await db.commit()
    if rows:
        logger.info("queued viewer reload commands after camera registry apply count=%d", len(rows))
    return len(rows)
