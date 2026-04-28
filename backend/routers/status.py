from fastapi import APIRouter
import psutil
import shutil
from services.recorder import get_active
from models import SystemStatus
from config import RECORDINGS_DIR

router = APIRouter(prefix="/api/status", tags=["status"])

@router.get("", response_model=SystemStatus)
async def get_status():
    disk = shutil.disk_usage(RECORDINGS_DIR)
    return SystemStatus(
        cpu_percent=psutil.cpu_percent(interval=0.1),
        ram_mb=psutil.virtual_memory().used / 1024 / 1024,
        disk_used_gb=disk.used / 1024**3,
        disk_total_gb=disk.total / 1024**3,
        cameras_online=len(get_active()),
    )
