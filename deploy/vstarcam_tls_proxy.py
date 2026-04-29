#!/usr/bin/env python3
"""
VSTARCAM 카메라 포트 443 TLS→평문 RTSP 프록시.
카메라의 포트 443(RTSP over TLS)을 로컬 평문 포트로 포워드.
"""
import socket, ssl, threading, sys, logging

logging.basicConfig(level=logging.INFO, format='%(asctime)s %(message)s')
log = logging.getLogger('tls-proxy')

CAMERAS = {
    10555: ('192.0.2.32', 443),  # camera-site-1
    10556: ('192.0.2.31', 443),  # camera-site-2
}

def relay(src, dst, label):
    try:
        while True:
            d = src.recv(16384)
            if not d:
                break
            dst.sendall(d)
    except Exception:
        pass

def handle(client, cam_host, cam_port):
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    try:
        raw = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        raw.settimeout(60)
        raw.connect((cam_host, cam_port))
        cam = ctx.wrap_socket(raw, server_hostname='ipcamera.vip')
    except Exception as e:
        log.warning('Camera connect failed: %s', e)
        client.close()
        return
    t1 = threading.Thread(target=relay, args=(client, cam, 'c→cam'), daemon=True)
    t2 = threading.Thread(target=relay, args=(cam, client, 'cam→c'), daemon=True)
    t1.start(); t2.start()
    t1.join(); t2.join()
    try: client.close()
    except: pass
    try: cam.close()
    except: pass

def serve(local_port, cam_host, cam_port):
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(('127.0.0.1', local_port))
    srv.listen(10)
    log.info('Proxy :127.0.0.1:%d → %s:%d', local_port, cam_host, cam_port)
    while True:
        try:
            conn, _ = srv.accept()
            threading.Thread(target=handle, args=(conn, cam_host, cam_port), daemon=True).start()
        except Exception as e:
            log.error('Accept error: %s', e)

threads = []
for local_port, (cam_host, cam_port) in CAMERAS.items():
    t = threading.Thread(target=serve, args=(local_port, cam_host, cam_port), daemon=False)
    t.start()
    threads.append(t)

for t in threads:
    t.join()
