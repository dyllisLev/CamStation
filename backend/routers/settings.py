from fastapi import APIRouter
from database import get_setting, set_setting
from models import Settings
from services.motion import set_motion_enabled
from services.cleaner import trigger_cleanup

router = APIRouter(prefix="/api/settings", tags=["settings"])

@router.get("", response_model=Settings)
async def get_settings():
    return Settings(
        retention_days=int(await get_setting("retention_days") or "30"),
        segment_minutes=int(await get_setting("segment_minutes") or "10"),
        motion_threshold=float(await get_setting("motion_threshold") or "0.02"),
        max_storage_gb=int(await get_setting("max_storage_gb") or "0"),
        motion_enabled=(await get_setting("motion_enabled") or "1") != "0",
    )

@router.post("", response_model=Settings)
async def update_settings(s: Settings):
    await set_setting("retention_days",   str(s.retention_days))
    await set_setting("segment_minutes",  str(s.segment_minutes))
    await set_setting("motion_threshold", str(s.motion_threshold))
    await set_setting("max_storage_gb",   str(s.max_storage_gb))
    await set_setting("motion_enabled",   "1" if s.motion_enabled else "0")
    set_motion_enabled(s.motion_enabled)
    trigger_cleanup()
    return s
