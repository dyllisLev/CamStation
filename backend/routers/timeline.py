from fastapi import APIRouter, Query
from pathlib import Path
import os
import aiosqlite
from datetime import datetime
from config import RECORDINGS_DIR, get_db_path

router = APIRouter(prefix="/api/timeline", tags=["timeline"])

async def scan_segments(cam_id: str, date_str: str, recordings_dir: str) -> list[dict]:
    day_dir = Path(recordings_dir) / cam_id / date_str
    if not day_dir.exists():
        return []
    files = sorted(day_dir.glob("*.mp4"))
    result = []
    for f in files:
        try:
            hh, mm = f.stem.split("-")
            dt = datetime.strptime(f"{date_str} {hh}:{mm}", "%Y-%m-%d %H:%M")
        except ValueError:
            continue
        result.append({
            "camera_id": cam_id,
            "date": date_str,
            "filename": f.name,
            "ts_start": dt.timestamp(),
        })
    return result

async def get_motion_events(cam_id: str, date_str: str, db_path: str) -> list[dict]:
    day_start = datetime.strptime(date_str, "%Y-%m-%d").timestamp()
    day_end = day_start + 86400
    async with aiosqlite.connect(db_path) as db:
        db.row_factory = aiosqlite.Row
        cur = await db.execute(
            "SELECT camera_id, ts_start, ts_end FROM motion_events "
            "WHERE camera_id=? AND ts_start>=? AND ts_start<? ORDER BY ts_start",
            (cam_id, day_start, day_end)
        )
        return [dict(row) for row in await cur.fetchall()]

@router.get("")
async def get_timeline(cam: str = Query(...), date: str = Query(...)):
    segments = await scan_segments(cam, date, RECORDINGS_DIR)
    events = await get_motion_events(cam, date, get_db_path())
    return {"segments": segments, "motion_events": events}
