"""Local signed rpm-md archive for actual zypper tests; no public fetching."""

import hashlib
import http.server
import json
from pathlib import Path
import ssl
import threading
import urllib.parse

ROOT = Path("/fixture/repo").resolve()
STATE = {"tamper_metadata": False, "tamper_package": False, "requests": []}
LOCK = threading.Lock()


class Repository(http.server.BaseHTTPRequestHandler):
    def log_message(self, _format, *args):
        pass

    def do_POST(self):
        path = urllib.parse.urlsplit(self.path).path
        modes = {"/__control/reset": (False, False),
                 "/__control/tamper-metadata": (True, False),
                 "/__control/tamper-package": (False, True)}
        if path not in modes:
            self.send_error(404)
            return
        with LOCK:
            metadata, package = modes[path]
            STATE.update(tamper_metadata=metadata, tamper_package=package, requests=[])
        self.send_response(204)
        self.end_headers()

    def do_HEAD(self):
        self.get(head=True)

    def do_GET(self):
        self.get()

    def get(self, head=False):
        path = urllib.parse.urlsplit(self.path).path
        if path == "/__control/status":
            with LOCK:
                body = json.dumps(STATE).encode()
            self.respond(body, "application/json", head)
            return
        if not path.startswith("/archive/fixture/"):
            self.send_error(404)
            return
        relative = path.removeprefix("/archive/fixture/")
        target = (ROOT / relative).resolve()
        if not target.is_relative_to(ROOT) or not target.is_file():
            self.send_error(404)
            return
        with LOCK:
            STATE["requests"].append({"path": path, "method": self.command,
                                      "user_agent": self.headers.get("User-Agent", "")})
            metadata = STATE["tamper_metadata"]
            package = STATE["tamper_package"]
        body = target.read_bytes()
        if metadata and relative == "repodata/repomd.xml":
            # Valid XML with an altered revision and the original detached
            # signature. A signature failure cannot be mistaken for XML damage.
            body = body.replace(b"<revision>", b"<revision>9", 1)
        if package and relative.endswith(".rpm"):
            # Same-length payload damage; signed metadata retains the valid
            # RPM's original checksum and the package signature is untouched.
            body = body[:-1] + bytes([body[-1] ^ 1])
        self.respond(body, "application/octet-stream", head)

    def respond(self, body, content_type, head):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "public, max-age=3600")
        self.send_header("ETag", '"' + hashlib.sha256(body).hexdigest() + '"')
        self.end_headers()
        if not head:
            self.wfile.write(body)


if __name__ == "__main__":
    control = http.server.ThreadingHTTPServer(("127.0.0.1", 8080), Repository)
    archive = http.server.ThreadingHTTPServer(("127.0.0.1", 8443), Repository)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain("/fixture/cert.pem", "/fixture/key.pem")
    archive.socket = context.wrap_socket(archive.socket, server_side=True)
    threading.Thread(target=archive.serve_forever, daemon=True).start()
    control.serve_forever()
