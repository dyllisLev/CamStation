from fastapi import APIRouter
import httpx
import logging
from config import GO2RTC_URL
from models import Camera

logger = logging.getLogger(__name__)
router = APIRouter(prefix="/api/cameras", tags=["cameras"])

@router.get("", response_model=list[Camera])
async def list_cameras():
    try:
        async with httpx.AsyncClient() as client:
            r = await client.get(f"{GO2RTC_URL}/api/streams", timeout=3)
            r.raise_for_status()
            streams = r.json()
    except Exception as e:
        logger.warning("go2rtc unreachable: %s", e)
        streams = {}

    cameras = []
    for cam_id, info in streams.items():
        if cam_id.endswith('_sub'):
            continue
        producers = info.get("producers") or []
        online = any("id" in p for p in producers)
        sub_info = streams.get(f"{cam_id}_sub")
        has_sub = online and sub_info is not None
        cameras.append(Camera(id=cam_id, name=cam_id, online=online, has_sub=has_sub))
    return cameras
