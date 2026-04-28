import asyncio
import subprocess
from fastapi import APIRouter
from pydantic import BaseModel
import httpx

router = APIRouter(prefix="/api/system", tags=["system"])

INSTALL_DIR = "/opt/camstation"
VERSION_FILE = f"{INSTALL_DIR}/.current-version"
TOKEN_FILE = f"{INSTALL_DIR}/.github-token"
DEPLOY_SCRIPT = f"{INSTALL_DIR}/deploy/deploy.sh"
GITHUB_REPO = "dyllisLev/CamStation"

_update_running = False


class SystemVersion(BaseModel):
    current_version: str
    latest_version: str | None
    update_available: bool


@router.get("/health")
async def health():
    return {"status": "ok"}


@router.get("/version", response_model=SystemVersion)
async def get_version():
    current = "unknown"
    try:
        with open(VERSION_FILE) as f:
            current = f.read().strip() or "unknown"
    except FileNotFoundError:
        pass

    latest = None
    try:
        token = open(TOKEN_FILE).read().strip()
        async with httpx.AsyncClient() as client:
            r = await client.get(
                f"https://api.github.com/repos/{GITHUB_REPO}/releases/latest",
                headers={
                    "Authorization": f"token {token}",
                    "Accept": "application/vnd.github+json",
                },
                timeout=5,
            )
            r.raise_for_status()
            latest = r.json()["tag_name"]
    except Exception:
        pass

    return SystemVersion(
        current_version=current,
        latest_version=latest,
        update_available=bool(latest and latest != current),
    )


@router.post("/update")
async def trigger_update():
    global _update_running
    if _update_running:
        return {"status": "already_running"}

    async def _run():
        global _update_running
        _update_running = True
        try:
            await asyncio.to_thread(
                subprocess.run,
                [DEPLOY_SCRIPT],
                capture_output=True,
            )
        finally:
            _update_running = False

    asyncio.create_task(_run())
    return {"status": "started"}
