from contextlib import asynccontextmanager
from fastapi import FastAPI
import asyncio
import httpx
import logging
from database import init_db, get_setting
from services import recorder
from services.cleaner import run_cleanup_loop
from services.motion import monitor_motion, set_motion_enabled
from config import GO2RTC_URL, RECORDINGS_DIR, get_db_path
from routers import system, cameras, streams, timeline, recordings as recordings_router, settings, status, layouts

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

@asynccontextmanager
async def lifespan(app: FastAPI):
    await init_db()
    try:
        async with httpx.AsyncClient() as client:
            r = await client.get(f"{GO2RTC_URL}/api/streams", timeout=5)
            r.raise_for_status()
            cam_ids = [k for k in r.json().keys() if not k.endswith('_sub')]
    except Exception as e:
        logger.warning("Could not fetch cameras on startup: %s", e)
        cam_ids = []

    segment_min = int(await get_setting("segment_minutes") or "10")
    motion_threshold = float(await get_setting("motion_threshold") or "0.02")
    motion_on = (await get_setting("motion_enabled") or "1") != "0"
    set_motion_enabled(motion_on)

    await recorder.start_all(cam_ids, segment_min, RECORDINGS_DIR)

    cleanup_task = asyncio.create_task(run_cleanup_loop(RECORDINGS_DIR, get_setting))
    motion_tasks = [
        asyncio.create_task(monitor_motion(cam_id, motion_threshold, get_db_path()))
        for cam_id in cam_ids
    ]

    yield

    for t in motion_tasks:
        t.cancel()
    cleanup_task.cancel()
    await recorder.stop_all()

app = FastAPI(lifespan=lifespan)

for r in [cameras.router, streams.router, timeline.router,
          recordings_router.router, settings.router, status.router, layouts.router, system.router]:
    app.include_router(r)
