import asyncio
import logging
import time

import aiosqlite
import httpx
from fastapi import APIRouter, HTTPException

from config import GO2RTC_CONFIG, GO2RTC_URL, RECORDINGS_DIR, TEMP_DIR, get_db_path
from database import get_setting
from models import Camera, CameraConfigStatus, CameraRebootResult, UpdateCameraEnabledRequest
from services import recorder
from services.camera_config import list_camera_configs, set_camera_enabled
from services.camera_registry import get_enabled_camera_ids as registry_enabled_camera_ids
from services.camera_registry import list_camera_admin_items, list_legacy_camera_config_status
from services.onvif_reboot import reboot_camera_via_onvif

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/api/cameras", tags=["cameras"])


async def _go2rtc_streams() -> dict:
    try:
        async with httpx.AsyncClient() as client:
            r = await client.get(f"{GO2RTC_URL}/api/streams", timeout=3)
            r.raise_for_status()
            return r.json()
    except Exception as e:
        logger.warning("go2rtc unreachable: %s", e)
        return {}


def _camera_from_stream(cam_id: str, info: dict, streams: dict) -> Camera:
    producers = info.get("producers") or []
    online = any("id" in p for p in producers)
    sub_info = streams.get(f"{cam_id}_sub")
    has_sub = online and sub_info is not None
    return Camera(id=cam_id, name=cam_id, online=online, has_sub=has_sub, enabled=True)


@router.get("", response_model=list[Camera])
async def list_cameras():
    streams = await _go2rtc_streams()
    registry_cameras = await list_camera_admin_items(streams=streams)
    if registry_cameras:
        return [
            Camera(
                id=camera.id,
                name=camera.display_name,
                online=camera.online,
                has_sub=camera.has_sub,
                enabled=camera.enabled,
            )
            for camera in registry_cameras
            if camera.enabled and not camera.archived
        ]

    cameras = []
    for cam_id, info in streams.items():
        if cam_id.endswith('_sub'):
            continue
        cameras.append(_camera_from_stream(cam_id, info, streams))
    return cameras


@router.get("/config", response_model=list[CameraConfigStatus])
async def list_camera_config():
    streams = await _go2rtc_streams()
    try:
        return await list_legacy_camera_config_status(streams=streams)
    except Exception as e:
        logger.exception("failed to read camera config: %s", e)
        raise HTTPException(status_code=500, detail="camera config read failed") from e


async def _restart_go2rtc() -> None:
    proc = await asyncio.create_subprocess_exec(
        "systemctl",
        "restart",
        "go2rtc",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    stdout, stderr = await proc.communicate()
    if proc.returncode != 0:
        raise RuntimeError((stderr or stdout).decode(errors="replace"))


async def _enqueue_viewer_reload(reason: str) -> None:
    now = time.time()
    async with aiosqlite.connect(get_db_path()) as db:
        rows = await db.execute_fetchall("SELECT client_id FROM viewer_clients")
        for row in rows:
            await db.execute(
                "INSERT INTO viewer_commands(client_id, command, status, reason, created_at) VALUES(?,?,?,?,?)",
                (row[0], "reload_page", "pending", reason, now),
            )
        await db.commit()


@router.patch("/{camera_id}/enabled", response_model=CameraConfigStatus)
async def update_camera_enabled(camera_id: str, payload: UpdateCameraEnabledRequest):
    try:
        set_camera_enabled(GO2RTC_CONFIG, camera_id, payload.enabled)
    except KeyError as e:
        raise HTTPException(status_code=404, detail="camera not found") from e
    except Exception as e:
        logger.exception("failed to update camera enabled state: %s", e)
        raise HTTPException(status_code=500, detail="camera config update failed") from e

    try:
        if not payload.enabled:
            await recorder.stop_recording(camera_id)
            await recorder.stop_sub_keepalive(camera_id)
        await _restart_go2rtc()
        if payload.enabled:
            segment_min = int(await get_setting("segment_minutes") or "10")
            await recorder.start_recording(camera_id, segment_min, RECORDINGS_DIR, TEMP_DIR)
            configs = {c.id: c for c in list_camera_configs(GO2RTC_CONFIG)}
            if configs.get(camera_id) and configs[camera_id].has_sub:
                await recorder.start_sub_keepalive(camera_id)
        await _enqueue_viewer_reload(f"{camera_id} {'활성화' if payload.enabled else '비활성화'} 반영")
    except Exception as e:
        logger.exception("failed to apply camera enabled state: %s", e)
        raise HTTPException(status_code=500, detail="camera state apply failed") from e

    cameras = await list_camera_config()
    for camera in cameras:
        if camera.id == camera_id:
            return camera
    raise HTTPException(status_code=404, detail="camera not found")


@router.post("/{camera_id}/reboot", response_model=CameraRebootResult)
async def reboot_camera(camera_id: str):
    try:
        target = await reboot_camera_via_onvif(GO2RTC_CONFIG, camera_id)
    except KeyError as e:
        raise HTTPException(status_code=404, detail="camera not found") from e
    except ValueError as e:
        raise HTTPException(status_code=400, detail=str(e)) from e
    except httpx.HTTPStatusError as e:
        logger.warning("ONVIF reboot failed for %s: HTTP %s", camera_id, e.response.status_code)
        raise HTTPException(status_code=502, detail=f"ONVIF reboot failed: HTTP {e.response.status_code}") from e
    except httpx.TimeoutException as e:
        logger.warning("ONVIF reboot timed out for %s", camera_id)
        raise HTTPException(status_code=504, detail="ONVIF reboot timed out") from e
    except Exception as e:
        logger.exception("ONVIF reboot failed for %s: %s", camera_id, e)
        raise HTTPException(status_code=500, detail="ONVIF reboot failed") from e

    return CameraRebootResult(
        camera_id=camera_id,
        endpoint=target.endpoint,
        message="ONVIF SystemReboot requested",
    )


def get_enabled_camera_ids() -> list[str]:
    try:
        import asyncio
        try:
            asyncio.get_running_loop()
        except RuntimeError:
            return asyncio.run(registry_enabled_camera_ids())
        return enabled_camera_ids_fallback()
    except Exception:
        logger.exception("failed to read enabled camera ids")
        return []


def enabled_camera_ids_fallback() -> list[str]:
    try:
        from services.camera_config import enabled_camera_ids
        return enabled_camera_ids(GO2RTC_CONFIG)
    except Exception:
        logger.exception("failed to read enabled camera ids from go2rtc config")
        return []
