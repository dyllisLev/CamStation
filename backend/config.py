import os

DB_PATH = os.environ.get("CAMSTATION_DB_PATH", "/opt/camstation/data/camstation.db")
GO2RTC_URL = os.environ.get("GO2RTC_URL", "http://127.0.0.1:1984")
RECORDINGS_DIR = os.environ.get("RECORDINGS_DIR", "/opt/camstation/recordings")
TEMP_DIR = os.environ.get("CAMSTATION_TEMP_DIR", "/opt/camstation/temp")
GO2RTC_CONFIG = os.environ.get("GO2RTC_CONFIG", "/opt/camstation/config/go2rtc.yaml")
RECORDING_HEALTH_CHECK_INTERVAL_SEC = int(os.environ.get("CAMSTATION_RECORDING_HEALTH_CHECK_INTERVAL_SEC", "300"))
HERMES_WEBHOOK_URL = os.environ.get("CAMSTATION_HERMES_WEBHOOK_URL", "")
HERMES_WEBHOOK_SECRET = os.environ.get("CAMSTATION_HERMES_WEBHOOK_SECRET", "")
ALERT_COOLDOWN_SEC = int(os.environ.get("CAMSTATION_ALERT_COOLDOWN_SEC", "300"))

def get_db_path() -> str:
    return os.environ.get("CAMSTATION_DB_PATH", DB_PATH)
