#!/usr/bin/env python3
"""
ONVIF 카메라 원격 재부팅 스크립트.

사용 시나리오:
  VSTARCAM 카메라가 RTSP 세션을 좀비 상태로 유지해 go2rtc가 연결을 못 할 때.
  증상: go2rtc 로그에 "connection reset by peer" 또는 "EOF" 반복, streams API에서
        해당 스트림에 id 필드 없음.

사용법:
  python3 deploy/onvif-reboot.py 192.0.2.32
  python3 deploy/onvif-reboot.py 192.0.2.32 --user admin --pass <REDACTED>
  python3 deploy/onvif-reboot.py 192.0.2.31 192.0.2.32   # 여러 카메라
"""

import argparse
import base64
import hashlib
import os
import socket
import sys
from datetime import datetime, timezone


def _ws_security_header(username: str, password: str) -> str:
    nonce = os.urandom(16)
    nonce_b64 = base64.b64encode(nonce).decode()
    created = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    digest = base64.b64encode(
        hashlib.sha1(nonce + created.encode() + password.encode()).digest()
    ).decode()
    return f"""<wsse:Security xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
      <wsse:UsernameToken>
        <wsse:Username>{username}</wsse:Username>
        <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">{digest}</wsse:Password>
        <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">{nonce_b64}</wsse:Nonce>
        <wsu:Created xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">{created}</wsu:Created>
      </wsse:UsernameToken>
    </wsse:Security>"""


def onvif_reboot(ip: str, port: int, username: str, password: str) -> bool:
    body = f"""<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
    xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <s:Header>{_ws_security_header(username, password)}</s:Header>
  <s:Body><tds:SystemReboot/></s:Body>
</s:Envelope>""".encode()

    http_req = (
        f"POST /onvif/device_service HTTP/1.1\r\n"
        f"Host: {ip}:{port}\r\n"
        f"Content-Type: application/soap+xml; charset=utf-8\r\n"
        f'SOAPAction: "http://www.onvif.org/ver10/device/wsdl/SystemReboot"\r\n'
        f"Content-Length: {len(body)}\r\n"
        f"Connection: close\r\n\r\n"
    ).encode() + body

    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(10)
        s.connect((ip, port))
        s.sendall(http_req)
        resp = b""
        while True:
            chunk = s.recv(4096)
            if not chunk:
                break
            resp += chunk
        s.close()
    except Exception as e:
        print(f"  [{ip}] 연결 실패: {e}", file=sys.stderr)
        return False

    if b"200 OK" in resp and b"SystemReboot" in resp:
        print(f"  [{ip}] 재부팅 명령 전송 완료 (응답 HTTP 200)")
        return True
    else:
        first_line = resp.split(b"\r\n")[0].decode(errors="replace") if resp else "(empty)"
        print(f"  [{ip}] 예상 밖 응답: {first_line}", file=sys.stderr)
        return False


def main():
    parser = argparse.ArgumentParser(description="ONVIF 카메라 원격 재부팅")
    parser.add_argument("ips", nargs="+", metavar="IP", help="카메라 IP 주소 (여러 개 가능)")
    parser.add_argument("--port", type=int, default=10080, help="ONVIF 포트 (기본값: 10080)")
    parser.add_argument("--user", default="admin", help="사용자명 (기본값: admin)")
    parser.add_argument("--pass", dest="password", default="<REDACTED>", help="비밀번호")
    args = parser.parse_args()

    success = 0
    for ip in args.ips:
        print(f"재부팅 중: {ip}:{args.port}")
        if onvif_reboot(ip, args.port, args.user, args.password):
            success += 1

    print(f"\n결과: {success}/{len(args.ips)} 성공")
    print("카메라가 완전히 재시작될 때까지 약 60초 소요됩니다.")
    print("이후 go2rtc가 자동으로 재연결합니다 (최대 1~2분).")
    sys.exit(0 if success == len(args.ips) else 1)


if __name__ == "__main__":
    main()
