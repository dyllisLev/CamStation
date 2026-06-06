import asyncio
import json
import logging
import time

import aiosqlite
from fastapi import APIRouter, HTTPException, Response, status

from config import get_db_path
from models import (
    CompleteViewerCommand,
    CreateViewerCommand,
    ViewerClientStatus,
    ViewerCommand,
    ViewerHeartbeat,
)

router = APIRouter(prefix="/api/viewers", tags=["viewers"])
_event_notifier = None
logger = logging.getLogger(__name__)


def configure_event_notifier(notifier) -> None:
    global _event_notifier
    _event_notifier = notifier


VIEWER_CAMERA_ACTIVITY_MAX_AGE_SEC = 30
VIEWER_DB_RETRY_ATTEMPTS = 3
VIEWER_DB_RETRY_BASE_DELAY_SEC = 0.05


async def _with_db_retry(operation, *, label: str):
    last_error: aiosqlite.OperationalError | None = None
    for attempt in range(1, VIEWER_DB_RETRY_ATTEMPTS + 1):
        try:
            return await operation()
        except aiosqlite.OperationalError as e:
            if "locked" not in str(e).lower() or attempt >= VIEWER_DB_RETRY_ATTEMPTS:
                raise
            last_error = e
            delay = VIEWER_DB_RETRY_BASE_DELAY_SEC * attempt
            logger.warning("Viewer DB operation locked label=%s attempt=%d retry_in=%.2fs", label, attempt, delay)
            await asyncio.sleep(delay)
    if last_error is not None:
        raise last_error
    raise RuntimeError(f"Viewer DB operation failed without exception label={label}")


def _camera_activity_recent(camera, now_ts: float) -> bool:
    for value in (camera.last_binary_at, camera.last_video_time_at):
        if value is not None and now_ts - float(value) <= VIEWER_CAMERA_ACTIVITY_MAX_AGE_SEC:
            return True
    return False


def _camera_is_healthy(camera, now_ts: float) -> bool:
    if not camera.connected or camera.error is not None or camera.stalled_ms >= 30_000:
        return False
    return camera.video_ready_state >= 2 or _camera_activity_recent(camera, now_ts)


def _state(expected_cameras: int, healthy_cameras: int) -> str:
    if expected_cameras <= 0:
        return "unknown"
    if healthy_cameras >= expected_cameras:
        return "healthy"
    return "degraded"


def _client_from_row(row) -> ViewerClientStatus:
    payload = json.loads(row[12] or "{}")
    cameras = payload.get("cameras") or []
    return ViewerClientStatus(
        client_id=row[0],
        name=row[1],
        app_version=row[2],
        server_url=row[3],
        platform=row[4],
        hostname=row[5],
        pid=row[6],
        started_at=row[7],
        last_seen=row[8],
        expected_cameras=row[9],
        healthy_cameras=row[10],
        state=row[11],
        cameras=cameras,
        payload=payload,
    )


def _command_from_row(row) -> ViewerCommand:
    result = json.loads(row[8]) if row[8] else None
    return ViewerCommand(
        id=row[0],
        client_id=row[1],
        command=row[2],
        status=row[3],
        reason=row[4],
        created_at=row[5],
        claimed_at=row[6],
        completed_at=row[7],
        result=result,
    )


@router.post("/heartbeat", response_model=ViewerClientStatus)
async def heartbeat(payload: ViewerHeartbeat):
    now = time.time()
    expected = payload.expected_cameras or len(payload.cameras)
    healthy = sum(1 for camera in payload.cameras if _camera_is_healthy(camera, now))
    state = _state(expected, healthy)
    payload_json = payload.model_dump_json()
    previous_state = None
    previous_healthy = None

    async def upsert_heartbeat():
        async with aiosqlite.connect(get_db_path()) as db:
            cur = await db.execute(
                "SELECT state, healthy_cameras FROM viewer_clients WHERE client_id=?",
                (payload.client_id,),
            )
            previous = await cur.fetchone()
            prev_state = previous[0] if previous is not None else None
            prev_healthy = int(previous[1] or 0) if previous is not None else None
            await db.execute(
                """
                INSERT INTO viewer_clients(
                    client_id, name, app_version, server_url, platform, hostname, pid,
                    started_at, last_seen, expected_cameras, healthy_cameras, state, payload_json
                ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
                ON CONFLICT(client_id) DO UPDATE SET
                    name=excluded.name,
                    app_version=excluded.app_version,
                    server_url=excluded.server_url,
                    platform=excluded.platform,
                    hostname=excluded.hostname,
                    pid=excluded.pid,
                    started_at=COALESCE(excluded.started_at, viewer_clients.started_at),
                    last_seen=excluded.last_seen,
                    expected_cameras=excluded.expected_cameras,
                    healthy_cameras=excluded.healthy_cameras,
                    state=excluded.state,
                    payload_json=excluded.payload_json
                """,
                (
                    payload.client_id,
                    payload.name,
                    payload.app_version,
                    payload.server_url,
                    payload.platform,
                    payload.hostname,
                    payload.pid,
                    payload.started_at,
                    now,
                    expected,
                    healthy,
                    state,
                    payload_json,
                ),
            )
            await db.commit()
            return prev_state, prev_healthy

    previous_state, previous_healthy = await _with_db_retry(upsert_heartbeat, label="viewer_heartbeat")

    if _event_notifier is not None:
        _event_notifier.notify_heartbeat(
            client_id=payload.client_id,
            state=state,
            previous_state=previous_state,
            healthy_cameras=healthy,
            previous_healthy_cameras=previous_healthy,
        )

    return await get_viewer(payload.client_id)


@router.get("", response_model=list[ViewerClientStatus])
async def list_viewers():
    async with aiosqlite.connect(get_db_path()) as db:
        cur = await db.execute(
            """
            SELECT client_id, name, app_version, server_url, platform, hostname, pid,
                   started_at, last_seen, expected_cameras, healthy_cameras, state, payload_json
            FROM viewer_clients
            ORDER BY last_seen DESC
            """
        )
        return [_client_from_row(row) for row in await cur.fetchall()]


@router.get("/{client_id}", response_model=ViewerClientStatus)
async def get_viewer(client_id: str):
    async with aiosqlite.connect(get_db_path()) as db:
        cur = await db.execute(
            """
            SELECT client_id, name, app_version, server_url, platform, hostname, pid,
                   started_at, last_seen, expected_cameras, healthy_cameras, state, payload_json
            FROM viewer_clients
            WHERE client_id=?
            """,
            (client_id,),
        )
        row = await cur.fetchone()
    if row is None:
        raise HTTPException(status_code=404, detail="viewer not found")
    return _client_from_row(row)


@router.post("/{client_id}/commands", response_model=ViewerCommand, status_code=status.HTTP_201_CREATED)
async def create_command(client_id: str, payload: CreateViewerCommand):
    now = time.time()
    async with aiosqlite.connect(get_db_path()) as db:
        cur = await db.execute(
            """
            INSERT INTO viewer_commands(client_id, command, status, reason, created_at)
            VALUES(?,?,?,?,?)
            """,
            (client_id, payload.command, "pending", payload.reason, now),
        )
        await db.commit()
        command_id = cur.lastrowid
        cur = await db.execute(
            """
            SELECT id, client_id, command, status, reason, created_at, claimed_at, completed_at, result_json
            FROM viewer_commands WHERE id=?
            """,
            (command_id,),
        )
        return _command_from_row(await cur.fetchone())


@router.get("/{client_id}/commands/pending", response_model=ViewerCommand | None)
async def claim_pending_commands(client_id: str):
    now = time.time()
    async with aiosqlite.connect(get_db_path()) as db:
        cur = await db.execute(
            """
            SELECT id FROM viewer_commands
            WHERE client_id=? AND status='pending'
            ORDER BY created_at ASC
            LIMIT 1
            """,
            (client_id,),
        )
        row = await cur.fetchone()
        if row is None:
            return Response(status_code=status.HTTP_204_NO_CONTENT)

        command_id = row[0]
        await db.execute(
            "UPDATE viewer_commands SET status='claimed', claimed_at=? WHERE id=?",
            (now, command_id),
        )
        await db.commit()
        cur = await db.execute(
            """
            SELECT id, client_id, command, status, reason, created_at, claimed_at, completed_at, result_json
            FROM viewer_commands
            WHERE client_id=? AND id=?
            """,
            (client_id, command_id),
        )
        return _command_from_row(await cur.fetchone())


@router.post("/{client_id}/commands/{command_id}/complete", response_model=ViewerCommand)
async def complete_command(client_id: str, command_id: int, payload: CompleteViewerCommand):
    now = time.time()
    result = {"ok": payload.ok, "message": payload.message, "details": payload.details}
    new_status = "completed" if payload.ok else "failed"
    async with aiosqlite.connect(get_db_path()) as db:
        await db.execute(
            """
            UPDATE viewer_commands
            SET status=?, completed_at=?, result_json=?
            WHERE id=? AND client_id=?
            """,
            (new_status, now, json.dumps(result, ensure_ascii=False), command_id, client_id),
        )
        await db.commit()
        cur = await db.execute(
            """
            SELECT id, client_id, command, status, reason, created_at, claimed_at, completed_at, result_json
            FROM viewer_commands WHERE id=? AND client_id=?
            """,
            (command_id, client_id),
        )
        row = await cur.fetchone()
    if row is None:
        raise HTTPException(status_code=404, detail="command not found")
    return _command_from_row(row)
