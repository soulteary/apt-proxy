"""An isolated signed pacman repository origin with observable package GETs."""

import hashlib
import http.server
import json
from pathlib import Path
import ssl
import threading
import urllib.parse

ROOT = Path("/fixture/repo").resolve()
REQUESTS = []
LOCK = threading.Lock()


class Repository(http.server.BaseHTTPRequestHandler):
    def log_message(self, _format, *args):
        pass

    def do_GET(self):
        self.respond(False)

    def do_HEAD(self):
        self.respond(True)

    def respond(self, head):
        path = urllib.parse.urlsplit(self.path).path
        if path == "/__control/status":
            with LOCK:
                body = json.dumps({"requests": list(REQUESTS)}).encode()
            cache_control = "no-store"
        else:
            with LOCK:
                REQUESTS.append({"method": self.command, "path": path})
            if path == "/pacman/health.txt":
                body = b"Local pacman fixture\n"
            else:
                target = (ROOT / path.lstrip("/")).resolve()
                if not target.is_relative_to(ROOT) or not target.is_file():
                    self.send_error(404)
                    return
                body = target.read_bytes()
            cache_control = "public, max-age=3600"
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", cache_control)
        self.send_header("ETag", '"' + hashlib.sha256(body).hexdigest() + '"')
        self.end_headers()
        if not head:
            self.wfile.write(body)


if __name__ == "__main__":
    plain = http.server.ThreadingHTTPServer(("127.0.0.1", 8080), Repository)
    tls = http.server.ThreadingHTTPServer(("127.0.0.1", 8443), Repository)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain("/fixture/cert.pem", "/fixture/key.pem")
    tls.socket = context.wrap_socket(tls.socket, server_side=True)
    threading.Thread(target=tls.serve_forever, daemon=True).start()
    plain.serve_forever()
