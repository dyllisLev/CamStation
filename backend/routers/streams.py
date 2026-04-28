from fastapi import APIRouter, Request
from fastapi.responses import StreamingResponse
import httpx
from config import GO2RTC_URL

router = APIRouter(prefix="/api/streams", tags=["streams"])

@router.post("/{cam_id}/webrtc")
async def webrtc_offer(cam_id: str, request: Request):
    body = await request.body()
    async with httpx.AsyncClient() as client:
        r = await client.post(
            f"{GO2RTC_URL}/api/webrtc?src={cam_id}",
            content=body,
            headers={"Content-Type": request.headers.get("content-type", "application/sdp")},
            timeout=10,
        )
    return StreamingResponse(
        iter([r.content]),
        status_code=r.status_code,
        media_type=r.headers.get("content-type", "application/sdp"),
    )
