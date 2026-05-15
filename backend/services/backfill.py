import aiosqlite
from pathlib import Path
from datetime import datetime, timezone, timedelta

KST = timezone(timedelta(hours=9))


def _parse_ts_start(filename: str, date_str: str) -> float | None:
    stem = Path(filename).stem
    # 신형: "HH-MM"
    parts = stem.split("-")
    if len(parts) == 2:
        try:
            hh, mm = parts
            dt = datetime.strptime(f"{date_str} {hh}:{mm}", "%Y-%m-%d %H:%M").replace(tzinfo=KST)
            return dt.timestamp()
        except ValueError:
            pass
    # 구형: "YYYY-MM-DD_HH-MM"
    if "_" in stem:
        try:
            date_part, time_part = stem.split("_", 1)
            hh, mm = time_part.split("-")
            dt = datetime.strptime(f"{date_part} {hh}:{mm}", "%Y-%m-%d %H:%M").replace(tzinfo=KST)
            return dt.timestamp()
        except (ValueError, IndexError):
            pass
    return None


async def backfill_recordings(recordings_dir: str, db_path: str, active_cam_ids: list[str]):
    base = Path(recordings_dir)
    if not base.exists():
        return

    today = datetime.now(KST).strftime("%Y-%m-%d")

    async with aiosqlite.connect(db_path) as db:
        for cam_dir in sorted(base.iterdir()):
            if not cam_dir.is_dir():
                continue
            cam_id = cam_dir.name

            for day_dir in sorted(cam_dir.iterdir()):
                if not day_dir.is_dir():
                    continue
                date_str = day_dir.name
                try:
                    datetime.strptime(date_str, "%Y-%m-%d")
                except ValueError:
                    continue

                parsed: list[tuple[Path, float]] = []
                for f in sorted(day_dir.glob("*.mp4")):
                    ts = _parse_ts_start(f.name, date_str)
                    if ts is not None:
                        parsed.append((f, ts))

                for i, (f, ts_start) in enumerate(parsed):
                    is_last = (i == len(parsed) - 1)
                    is_active_today = (cam_id in active_cam_ids and date_str == today)

                    if is_last and is_active_today:
                        ts_end = None
                    elif is_last:
                        ts_end = f.stat().st_mtime
                    else:
                        ts_end = parsed[i + 1][1]

                    file_size = f.stat().st_size

                    await db.execute(
                        "INSERT OR IGNORE INTO recordings"
                        "(camera_id, filename, ts_start, ts_end, file_size) VALUES(?,?,?,?,?)",
                        (cam_id, f.name, ts_start, ts_end, file_size),
                    )
                    if ts_end is None:
                        await db.execute(
                            "UPDATE recordings SET file_size=? "
                            "WHERE camera_id=? AND ts_start=? AND file_size IS NULL",
                            (file_size, cam_id, ts_start),
                        )
                    else:
                        await db.execute(
                            "UPDATE recordings SET ts_end=?, file_size=? "
                            "WHERE camera_id=? AND ts_start=? "
                            "AND (ts_end IS NULL OR file_size IS NULL)",
                            (ts_end, file_size, cam_id, ts_start),
                        )

        await db.commit()
