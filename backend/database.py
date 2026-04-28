from typing import Optional
import aiosqlite
from config import get_db_path

async def init_db():
    async with aiosqlite.connect(get_db_path()) as db:
        await db.execute("PRAGMA journal_mode=WAL")
        await db.execute("PRAGMA foreign_keys=ON")
        await db.executescript("""
            CREATE TABLE IF NOT EXISTS motion_events (
                id        INTEGER PRIMARY KEY AUTOINCREMENT,
                camera_id TEXT    NOT NULL,
                ts_start  REAL    NOT NULL,
                ts_end    REAL,
                created   REAL    DEFAULT (unixepoch())
            );
            CREATE INDEX IF NOT EXISTS idx_motion_cam_ts
                ON motion_events(camera_id, ts_start);

            CREATE TABLE IF NOT EXISTS settings (
                key   TEXT PRIMARY KEY,
                value TEXT NOT NULL
            );
            INSERT OR IGNORE INTO settings(key, value) VALUES
                ('retention_days',   '30'),
                ('segment_minutes',  '10'),
                ('motion_threshold', '0.02'),
                ('max_storage_gb',   '0'),
                ('motion_enabled',   '1');

            CREATE TABLE IF NOT EXISTS layouts (
                id                 TEXT    PRIMARY KEY,
                name               TEXT    NOT NULL,
                data               TEXT    NOT NULL,
                timeline_collapsed INTEGER NOT NULL DEFAULT 0,
                created_at         INTEGER NOT NULL,
                updated_at         INTEGER NOT NULL
            );

            CREATE TABLE IF NOT EXISTS recordings (
                id        INTEGER PRIMARY KEY AUTOINCREMENT,
                camera_id TEXT NOT NULL,
                filename  TEXT NOT NULL,
                ts_start  REAL NOT NULL,
                ts_end    REAL,
                file_size INTEGER,
                created   REAL DEFAULT (unixepoch()),
                UNIQUE(camera_id, filename)
            );
            CREATE INDEX IF NOT EXISTS idx_rec_cam_ts
                ON recordings(camera_id, ts_start);
        """)
        await db.commit()

async def get_setting(key: str) -> Optional[str]:
    async with aiosqlite.connect(get_db_path()) as db:
        cur = await db.execute("SELECT value FROM settings WHERE key=?", (key,))
        row = await cur.fetchone()
        return row[0] if row else None

async def set_setting(key: str, value: str):
    async with aiosqlite.connect(get_db_path()) as db:
        await db.execute(
            "INSERT OR REPLACE INTO settings(key, value) VALUES(?,?)",
            (key, value)
        )
        await db.commit()
