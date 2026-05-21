import logging

import httpx
from fastapi import APIRouter, HTTPException, status

from config import GO2RTC_CONFIG, GO2RTC_URL
from models import CameraAdminItem, CameraCreateRequest, CameraUpdateRequest, UpdateCameraEnabledRequest
from services.camera_registry import (
    archive_camera,
    create_camera,
    get_enabled_camera_ids,
    get_enabled_sub_camera_ids,
    list_camera_admin_items,
    set_camera_enabled,
    update_camera,
)
from services.go2rtc_config_writer import write_go2rtc_config_from_db
from services import camera_runtime_apply

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/api/camera-admin", tags=["camera-admin"])


async def _go2rtc_streams() -> dict:
    try:
        async with httpx.AsyncClient() as client:
            response = await client.get(f"{GO2RTC_URL}/api/streams", timeout=3)
            response.raise_for_status()
            return response.json()
    except Exception as exc:
        logger.warning("go2rtc unreachable while listing camera admin items: %s", exc)
        return {}


@router.get("", response_model=list[CameraAdminItem])
async def list_camera_admin():
    try:
        return await list_camera_admin_items(streams=await _go2rtc_streams())
    except Exception as exc:
        logger.exception("failed to list camera admin items: %s", exc)
        raise HTTPException(status_code=500, detail="camera admin list failed") from exc


@router.post("", response_model=CameraAdminItem, status_code=status.HTTP_201_CREATED)
async def create_camera_admin(payload: CameraCreateRequest):
    try:
        return await create_camera(payload)
    except ValueError as exc:
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except Exception as exc:
        logger.exception("failed to create camera: %s", exc)
        raise HTTPException(status_code=500, detail="camera create failed") from exc


@router.patch("/{camera_id}", response_model=CameraAdminItem)
async def update_camera_admin(camera_id: str, payload: CameraUpdateRequest):
    try:
        return await update_camera(camera_id, payload)
    except KeyError as exc:
        raise HTTPException(status_code=404, detail="camera not found") from exc
    except Exception as exc:
        logger.exception("failed to update camera %s: %s", camera_id, exc)
        raise HTTPException(status_code=500, detail="camera update failed") from exc


@router.post("/{camera_id}/enabled", response_model=CameraAdminItem)
async def set_camera_admin_enabled(camera_id: str, payload: UpdateCameraEnabledRequest):
    try:
        return await set_camera_enabled(camera_id, payload.enabled)
    except KeyError as exc:
        raise HTTPException(status_code=404, detail="camera not found") from exc
    except Exception as exc:
        logger.exception("failed to set camera enabled %s: %s", camera_id, exc)
        raise HTTPException(status_code=500, detail="camera enabled update failed") from exc


@router.delete("/{camera_id}", response_model=CameraAdminItem)
async def archive_camera_admin(camera_id: str):
    try:
        return await archive_camera(camera_id)
    except KeyError as exc:
        raise HTTPException(status_code=404, detail="camera not found") from exc
    except Exception as exc:
        logger.exception("failed to archive camera %s: %s", camera_id, exc)
        raise HTTPException(status_code=500, detail="camera archive failed") from exc


@router.post("/apply")
async def apply_camera_registry_to_go2rtc_config():
    try:
        result = await write_go2rtc_config_from_db(GO2RTC_CONFIG)
        go2rtc_restarted = False
        recorders_reconciled = False
        viewer_reload_commands = 0
        if result.changed:
            await camera_runtime_apply.restart_go2rtc()
            go2rtc_restarted = True
            camera_ids = await get_enabled_camera_ids()
            sub_camera_ids = await get_enabled_sub_camera_ids()
            await camera_runtime_apply.reconcile_recorders(camera_ids, sub_camera_ids)
            recorders_reconciled = True
            viewer_reload_commands = await camera_runtime_apply.enqueue_viewer_reload_commands()
    except Exception as exc:
        logger.exception("failed to apply camera registry to go2rtc config: %s", exc)
        raise HTTPException(status_code=500, detail="camera config apply failed") from exc
    return {
        "changed": result.changed,
        "backup_created": result.backup_path is not None,
        "go2rtc_restarted": go2rtc_restarted,
        "recorders_reconciled": recorders_reconciled,
        "viewer_reload_commands": viewer_reload_commands,
    }
