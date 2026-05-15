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
                grid_cols          INTEGER NOT NULL DEFAULT 12,
                grid_rows          INTEGER,
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
                UNIQUE(camera_id, ts_start)
            );
            CREATE INDEX IF NOT EXISTS idx_rec_cam_ts
                ON recordings(camera_id, ts_start);

            CREATE TABLE IF NOT EXISTS viewer_clients (
                client_id        TEXT PRIMARY KEY,
                name             TEXT NOT NULL,
                app_version      TEXT,
                server_url       TEXT,
                platform         TEXT,
                hostname         TEXT,
                pid              INTEGER,
                started_at       REAL,
                last_seen        REAL NOT NULL,
                expected_cameras INTEGER NOT NULL DEFAULT 0,
                healthy_cameras  INTEGER NOT NULL DEFAULT 0,
                state            TEXT NOT NULL DEFAULT 'unknown',
                payload_json     TEXT NOT NULL DEFAULT '{}'
            );
            CREATE INDEX IF NOT EXISTS idx_viewer_clients_last_seen
                ON viewer_clients(last_seen);

            CREATE TABLE IF NOT EXISTS viewer_commands (
                id           INTEGER PRIMARY KEY AUTOINCREMENT,
                client_id    TEXT NOT NULL,
                command      TEXT NOT NULL,
                status       TEXT NOT NULL DEFAULT 'pending',
                reason       TEXT,
                created_at   REAL NOT NULL,
                claimed_at   REAL,
                completed_at REAL,
                result_json  TEXT
            );
            CREATE INDEX IF NOT EXISTS idx_viewer_commands_client_status
                ON viewer_commands(client_id, status, created_at);
        """)
        cursor = await db.execute("PRAGMA table_info(layouts)")
        columns = {row[1] for row in await cursor.fetchall()}
        if "grid_cols" not in columns:
            await db.execute("ALTER TABLE layouts ADD COLUMN grid_cols INTEGER NOT NULL DEFAULT 12")
        if "grid_rows" not in columns:
            await db.execute("ALTER TABLE layouts ADD COLUMN grid_rows INTEGER")
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
