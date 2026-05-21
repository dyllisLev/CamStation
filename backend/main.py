from contextlib import asynccontextmanager
from fastapi import FastAPI
import asyncio
import httpx
import logging
from database import init_db, get_setting
from services import recorder
from services.cleaner import run_cleanup_loop
from services.motion import monitor_motion, set_motion_enabled
from services.recording_health import run_recording_health_loop
from services.viewer_health import ViewerHealthEventNotifier, run_viewer_health_loop
from services.webhook_alerts import WebhookAlertSender
from config import (
    GO2RTC_URL,
    RECORDINGS_DIR,
    TEMP_DIR,
    RECORDING_HEALTH_CHECK_INTERVAL_SEC,
    VIEWER_HEALTH_CHECK_INTERVAL_SEC,
    VIEWER_HEALTH_MAX_AGE_SEC,
    HERMES_WEBHOOK_URL,
    HERMES_WEBHOOK_SECRET,
    ALERT_COOLDOWN_SEC,
    get_db_path,
)
from routers import system, cameras, streams, timeline, recordings as recordings_router, settings, status, layouts, viewers

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


async def _start_sub_keepalives(sub_cam_ids: list[str]):
    for cam_id in sub_cam_ids:
        await recorder.start_sub_keepalive(cam_id)
        await asyncio.sleep(1)
    logger.info("Sub-stream keepalive tasks started for %d cameras", len(sub_cam_ids))


def _startup_camera_lists(all_stream_keys: list[str], enabled_ids: list[str]) -> tuple[list[str], list[str]]:
    """Return startup recorder/keepalive camera lists using config-enabled cameras only.

    go2rtc can briefly expose stale streams during service/config transitions.  The
    CamStation backend must treat go2rtc.yaml enabled state as authoritative so a
    disabled camera never restarts recording or participates in health alerts.
    """
    stream_keys = set(all_stream_keys)
    main_streams = {k for k in stream_keys if not k.endswith("_sub")}
    if main_streams:
        cam_ids = [cam_id for cam_id in enabled_ids if cam_id in main_streams]
    else:
        cam_ids = list(enabled_ids)
    sub_cam_ids = [cam_id for cam_id in cam_ids if f"{cam_id}_sub" in stream_keys]
    return cam_ids, sub_cam_ids


@asynccontextmanager
async def lifespan(app: FastAPI):
    await init_db()
    enabled_ids = cameras.get_enabled_camera_ids()
    all_keys = []
    try:
        async with httpx.AsyncClient() as client:
            r = await client.get(f"{GO2RTC_URL}/api/streams", timeout=5)
            r.raise_for_status()
            all_keys = list(r.json().keys())
    except Exception as e:
        logger.warning("Could not fetch cameras on startup: %s", e)
    cam_ids, sub_cam_ids = _startup_camera_lists(all_keys, enabled_ids)
    logger.info("Startup enabled cameras=%d go2rtc_streams=%d", len(cam_ids), len(all_keys))

    segment_min = int(await get_setting("segment_minutes") or "10")
    motion_threshold = float(await get_setting("motion_threshold") or "0.02")
    motion_on = (await get_setting("motion_enabled") or "1") != "0"
    set_motion_enabled(motion_on)

    from services.backfill import backfill_recordings
    await backfill_recordings(RECORDINGS_DIR, get_db_path(), active_cam_ids=[])

    await recorder.start_all(cam_ids, segment_min, RECORDINGS_DIR, TEMP_DIR)
    sub_task = asyncio.create_task(_start_sub_keepalives(sub_cam_ids))

    cleanup_task = asyncio.create_task(run_cleanup_loop(RECORDINGS_DIR, get_setting))
    alert_sender = WebhookAlertSender(
        url=HERMES_WEBHOOK_URL,
        secret=HERMES_WEBHOOK_SECRET,
        cooldown_sec=ALERT_COOLDOWN_SEC,
    )
    health_task = asyncio.create_task(
        run_recording_health_loop(
            cam_ids,
            RECORDINGS_DIR,
            TEMP_DIR,
            get_db_path(),
            get_active_cam_ids=recorder.get_active,
            get_segment_minutes=lambda: get_setting("segment_minutes"),
            get_camera_ids=cameras.get_enabled_camera_ids,
            get_skip_reason=recorder.get_maintenance_reason,
            interval_sec=RECORDING_HEALTH_CHECK_INTERVAL_SEC,
            alert_sender=alert_sender,
        )
    )
    viewer_event_notifier = ViewerHealthEventNotifier(
        get_db_path(),
        alert_sender=alert_sender,
        max_heartbeat_age_sec=VIEWER_HEALTH_MAX_AGE_SEC,
        get_enabled_camera_ids=cameras.get_enabled_camera_ids,
    )
    viewers.configure_event_notifier(viewer_event_notifier)
    recorder.set_event_alert_sender(alert_sender)
    viewer_health_task = asyncio.create_task(
        run_viewer_health_loop(
            get_db_path(),
            interval_sec=VIEWER_HEALTH_CHECK_INTERVAL_SEC,
            max_heartbeat_age_sec=VIEWER_HEALTH_MAX_AGE_SEC,
            alert_sender=alert_sender,
            get_enabled_camera_ids=cameras.get_enabled_camera_ids,
        )
    )
    motion_tasks = [
        asyncio.create_task(monitor_motion(cam_id, motion_threshold, get_db_path()))
        for cam_id in cam_ids
    ]

    yield

    for t in motion_tasks:
        t.cancel()
    await viewer_event_notifier.close()
    viewers.configure_event_notifier(None)
    recorder.set_event_alert_sender(None)
    cleanup_task.cancel()
    health_task.cancel()
    viewer_health_task.cancel()
    sub_task.cancel()
    await recorder.stop_all()

app = FastAPI(lifespan=lifespan)

for r in [cameras.router, streams.router, timeline.router,
          recordings_router.router, settings.router, status.router, layouts.router, viewers.router, system.router]:
    app.include_router(r)
