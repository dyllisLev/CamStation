from __future__ import annotations

import base64
import hashlib
import html
import os
import re
import secrets
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse, unquote

import httpx


@dataclass(slots=True)
class CameraOnvifTarget:
    camera_id: str
    username: str
    password: str
    host: str
    port: int

    @property
    def endpoint(self) -> str:
        return f"http://{self.host}:{self.port}/onvif/device_service"


# Verified/known ONVIF management ports for the installed cameras.
_ONVIF_PORT_BY_HOST = {
    "192.0.2.81": 2020,
    "192.0.2.4": 2020,
    "192.0.2.9": 2020,
    "192.0.2.32": 10080,
    "192.0.2.31": 10080,
    "192.0.2.12": 8000,
    "192.0.2.55": 10080,
}

_STREAM_LINE_RE = re.compile(r"^(?:#\s*)?\s{2,}(?P<name>[^#\s][^:]*):\s*(?P<value>.*)$")


def _clean_stream_value(value: str) -> str:
    value = value.strip()
    if (value.startswith('"') and value.endswith('"')) or (value.startswith("'") and value.endswith("'")):
        value = value[1:-1]
    if value.startswith("ffmpeg:"):
        value = value[len("ffmpeg:"):]
    if "#" in value:
        value = value.split("#", 1)[0]
    return value.strip()


def _camera_rtsp_url(config_path: str, camera_id: str) -> str:
    path = Path(config_path)
    for line in path.read_text(encoding="utf-8").splitlines():
        match = _STREAM_LINE_RE.match(line)
        if not match:
            continue
        if match.group("name").strip() == camera_id:
            value = _clean_stream_value(match.group("value"))
            if value.startswith("rtsp://"):
                return value
            break
    raise KeyError(camera_id)


def resolve_onvif_target(config_path: str, camera_id: str) -> CameraOnvifTarget:
    rtsp_url = _camera_rtsp_url(config_path, camera_id)
    parsed = urlparse(rtsp_url)
    if not parsed.hostname or not parsed.username or parsed.password is None:
        raise ValueError("camera RTSP URL is missing host or credentials")
    port = _ONVIF_PORT_BY_HOST.get(parsed.hostname)
    if port is None:
        raise ValueError(f"ONVIF reboot port is not configured for {parsed.hostname}")
    return CameraOnvifTarget(
        camera_id=camera_id,
        username=unquote(parsed.username),
        password=unquote(parsed.password),
        host=parsed.hostname,
        port=port,
    )


def _wsse_header(username: str, password: str) -> str:
    nonce = secrets.token_bytes(16)
    created = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    digest = base64.b64encode(hashlib.sha1(nonce + created.encode("utf-8") + password.encode("utf-8")).digest()).decode("ascii")
    nonce_b64 = base64.b64encode(nonce).decode("ascii")
    return f"""
    <s:Header>
      <wsse:Security s:mustUnderstand="1" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
        <wsse:UsernameToken>
          <wsse:Username>{html.escape(username)}</wsse:Username>
          <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">{digest}</wsse:Password>
          <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">{nonce_b64}</wsse:Nonce>
          <wsu:Created>{created}</wsu:Created>
        </wsse:UsernameToken>
      </wsse:Security>
    </s:Header>"""


def _system_reboot_envelope(username: str, password: str) -> str:
    return f"""<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
{_wsse_header(username, password)}
  <s:Body>
    <tds:SystemReboot/>
  </s:Body>
</s:Envelope>"""


async def reboot_camera_via_onvif(config_path: str, camera_id: str, timeout_sec: float | None = None) -> CameraOnvifTarget:
    target = resolve_onvif_target(config_path, camera_id)
    timeout = timeout_sec if timeout_sec is not None else float(os.environ.get("CAMSTATION_ONVIF_REBOOT_TIMEOUT_SEC", "8"))
    envelope = _system_reboot_envelope(target.username, target.password)
    async with httpx.AsyncClient(timeout=timeout) as client:
        response = await client.post(
            target.endpoint,
            content=envelope.encode("utf-8"),
            headers={
                "Content-Type": "application/soap+xml; charset=utf-8; action=\"http://www.onvif.org/ver10/device/wsdl/SystemReboot\"",
            },
        )
        response.raise_for_status()
    return target
