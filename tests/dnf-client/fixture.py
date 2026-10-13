"""Local rpm-md/Metalink origin; never fetches public repositories."""

import hashlib
import http.server
import json
from pathlib import Path
import ssl
import threading
import urllib.parse
import xml.etree.ElementTree as ET


ROOT = Path("/fixture/repo").resolve()
NAMESPACE = "http://www.metalinker.org/"
TIMESTAMP_NAMESPACE = "http://fedorahosted.org/mirrormanager"
STATE = {"bad_hash": False, "fail_first": False, "requests": []}
LOCK = threading.Lock()
ET.register_namespace("", NAMESPACE)
ET.register_namespace("mm0", TIMESTAMP_NAMESPACE)


def metalink():
    repomd = (ROOT / "repodata/repomd.xml").read_bytes()
    with LOCK:
        bad_hash = STATE["bad_hash"]
    document = ET.Element("{" + NAMESPACE + "}metalink", version="3.0", type="dynamic")
    files = ET.SubElement(document, "files")
    file = ET.SubElement(files, "file", name="repomd.xml")
    ET.SubElement(file, "size").text = str(len(repomd))
    ET.SubElement(file, "{" + TIMESTAMP_NAMESPACE + "}timestamp").text = str(
        int((ROOT / "repodata/repomd.xml").stat().st_mtime)
    )
    verification = ET.SubElement(file, "verification")
    ET.SubElement(verification, "hash", type="sha256").text = (
        "0" * 64 if bad_hash else hashlib.sha256(repomd).hexdigest()
    )
    resources = ET.SubElement(file, "resources", maxconnections="1")
    for mirror, preference in (("a", "100"), ("b", "90")):
        ET.SubElement(resources, "url", protocol="https", type="https",
                      location="US", preference=preference).text = (
            "https://127.0.0.1:8443/mirror-" + mirror + "/repodata/repomd.xml"
        )
    return ET.tostring(document, encoding="utf-8", xml_declaration=True)


class Repository(http.server.BaseHTTPRequestHandler):
    def log_message(self, _format, *args):
        pass

    def do_POST(self):
        path = urllib.parse.urlsplit(self.path).path
        if path not in ("/__control/reset", "/__control/fail-first", "/__control/bad-hash"):
            self.send_error(404)
            return
        with LOCK:
            if path == "/__control/reset":
                STATE.update(bad_hash=False, fail_first=False, requests=[])
            elif path == "/__control/fail-first":
                STATE["fail_first"] = True
            else:
                STATE.update(bad_hash=True, fail_first=False, requests=[])
        self.send_response(204)
        self.end_headers()

    def do_HEAD(self):
        self.get(head=True)

    def do_GET(self):
        self.get()

    def get(self, head=False):
        parsed = urllib.parse.urlsplit(self.path)
        path = parsed.path
        if path == "/__control/status":
            with LOCK:
                body = json.dumps(STATE).encode()
            self.respond(body, "application/json", head)
            return
        with LOCK:
            STATE["requests"].append({"path": path, "query": parsed.query,
                                      "method": self.command,
                                      "user_agent": self.headers.get("User-Agent", "")})
            fail_first = STATE["fail_first"]
        if path == "/metalink":
            query = urllib.parse.parse_qs(parsed.query)
            # Verify that DNF expands its native repository variables and that
            # apt-proxy preserves the source's query parameters.
            if (query.get("repo") != ["centos-baseos-" + Path("/fixture/stream").read_text().strip() + "-stream"]
                    or query.get("arch") != [Path("/fixture/arch").read_text().strip()]
                    or query.get("protocol") != ["https,http"]):
                self.send_error(400, "Unexpected Metalink query")
                return
            # Dynamic discovery may require revalidation on every request.
            # The proxy's artifact cache must still reuse identical RPMs.
            self.respond(metalink(), "application/metalink+xml", head, "no-cache")
            return
        if not path.startswith(("/mirror-a/", "/mirror-b/")):
            self.send_error(404)
            return
        relative = path.split("/", 2)[2]
        target = (ROOT / relative).resolve()
        if not target.is_relative_to(ROOT) or not target.is_file():
            self.send_error(404)
            return
        if fail_first and path == "/mirror-a/repodata/repomd.xml":
            self.send_error(503, "Preferred mirror unavailable")
            return
        self.respond(target.read_bytes(), "application/octet-stream", head)

    def respond(self, body, content_type, head, cache_control="public, max-age=3600"):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
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
