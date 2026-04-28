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


async def _start_sub_keepalives(sub_cam_ids: list[str]):
    """메인 스트림이 연결된 후 순차적으로 서브 스트림 keepalive를 시작한다."""
    if not sub_cam_ids:
        return
    for _ in range(30):
        await asyncio.sleep(2)
        try:
            async with httpx.AsyncClient() as client:
                r = await client.get(f"{GO2RTC_URL}/api/streams", timeout=3)
                streams = r.json()
            if all(
                any("id" in p for p in streams.get(cam_id, {}).get("producers", []))
                for cam_id in sub_cam_ids
            ):
                break
        except Exception:
            pass
    for cam_id in sub_cam_ids:
        await recorder.start_sub_keepalive(cam_id)
        await asyncio.sleep(1)
    logger.info("Sub-stream keepalives started for %d cameras", len(sub_cam_ids))


@asynccontextmanager
async def lifespan(app: FastAPI):
    await init_db()
    cam_ids = []
    sub_cam_ids = []
    try:
        async with httpx.AsyncClient() as client:
            r = await client.get(f"{GO2RTC_URL}/api/streams", timeout=5)
            r.raise_for_status()
            all_keys = list(r.json().keys())
            cam_ids = [k for k in all_keys if not k.endswith('_sub')]
            sub_cam_ids = [k[:-4] for k in all_keys if k.endswith('_sub')]
    except Exception as e:
        logger.warning("Could not fetch cameras on startup: %s", e)

    segment_min = int(await get_setting("segment_minutes") or "10")
    motion_threshold = float(await get_setting("motion_threshold") or "0.02")
    motion_on = (await get_setting("motion_enabled") or "1") != "0"
    set_motion_enabled(motion_on)

    await recorder.start_all(cam_ids, segment_min, RECORDINGS_DIR)
    sub_task = asyncio.create_task(_start_sub_keepalives(sub_cam_ids))

    cleanup_task = asyncio.create_task(run_cleanup_loop(RECORDINGS_DIR, get_setting))
    motion_tasks = [
        asyncio.create_task(monitor_motion(cam_id, motion_threshold, get_db_path()))
        for cam_id in cam_ids
    ]

    yield

    for t in motion_tasks:
        t.cancel()
    cleanup_task.cancel()
    sub_task.cancel()
    await recorder.stop_all()

app = FastAPI(lifespan=lifespan)

for r in [cameras.router, streams.router, timeline.router,
          recordings_router.router, settings.router, status.router, layouts.router, system.router]:
    app.include_router(r)
