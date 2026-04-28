from fastapi import APIRouter, Query
from datetime import datetime, timezone, timedelta
import aiosqlite
from config import get_db_path

router = APIRouter(prefix="/api/timeline", tags=["timeline"])

KST = timezone(timedelta(hours=9))


async def get_segments_from_db(cam_id: str, date_str: str, db_path: str) -> list[dict]:
    day_start = datetime.strptime(date_str, "%Y-%m-%d").replace(tzinfo=KST).timestamp()
    day_end = day_start + 86400
    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute(
            "SELECT camera_id, filename, ts_start, ts_end, file_size FROM recordings "
            "WHERE camera_id=? AND ts_start>=? AND ts_start<? ORDER BY ts_start",
            (cam_id, day_start, day_end),
        )
        return [dict(row) for row in await cur.fetchall()]


async def get_motion_events(cam_id: str, date_str: str, db_path: str) -> list[dict]:
    day_start = datetime.strptime(date_str, "%Y-%m-%d").replace(tzinfo=KST).timestamp()
    day_end = day_start + 86400
    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute(
            "SELECT camera_id, ts_start, ts_end FROM motion_events "
            "WHERE camera_id=? AND ts_start>=? AND ts_start<? ORDER BY ts_start",
            (cam_id, day_start, day_end),
        )
        return [dict(row) for row in await cur.fetchall()]


@router.get("")
async def get_timeline(cam: str = Query(...), date: str = Query(...)):
    db_path = get_db_path()
    segments = await get_segments_from_db(cam, date, db_path)
    events = await get_motion_events(cam, date, db_path)
    return {"segments": segments, "motion_events": events}
