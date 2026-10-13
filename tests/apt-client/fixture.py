"""Local signed-repository HTTP/TLS fixture; never fetches the public internet."""
import base64
import hashlib
import http.server
import json
import pathlib
import socket
import ssl
import threading
import urllib.parse

ROOT = pathlib.Path('/fixture/repo').resolve()
STATE = {'fault': '', 'counts': {}, 'paths': [], 'requests': [], 'private': 0, 'ranges': 0}
LOCK = threading.Lock()


class Repository(http.server.BaseHTTPRequestHandler):
    def log_message(self, _format, *args):
        pass

    def do_POST(self):
        path = urllib.parse.urlsplit(self.path).path
        if path not in ('/__control/reset', '/__control/interrupt', '/__control/fail'):
            self.send_error(404)
            return
        with LOCK:
            STATE['fault'] = path.rsplit('/', 1)[-1] if not path.endswith('reset') else ''
        self.send_response(204)
        self.end_headers()

    def do_HEAD(self):
        self.get(head=True)

    def do_GET(self):
        self.get()

    def get(self, head=False):
        path = urllib.parse.urlsplit(self.path).path
        if path == '/__control/status':
            with LOCK:
                body = json.dumps(STATE).encode()
            self.send_response(200)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            if not head:
                self.wfile.write(body)
            return
        if path.startswith('/private/'):
            if self.headers.get('Authorization') != 'Basic ' + base64.b64encode(b'fixture:fixture-password').decode():
                self.send_response(401)
                self.send_header('WWW-Authenticate', 'Basic realm="APT fixture"')
                self.send_header('Content-Length', '0')
                self.end_headers()
                return
            with LOCK:
                STATE['private'] += 1
            path = path.removeprefix('/private')
        target = ROOT / path.lstrip('/')
        if not target.resolve().is_relative_to(ROOT) or not target.is_file():
            self.send_error(404)
            return
        body = target.read_bytes()
        etag = '"' + hashlib.sha256(body).hexdigest() + '"'
        with LOCK:
            STATE['paths'].append(path)
            STATE['requests'].append({'method': 'HEAD' if head else 'GET', 'path': path})
            STATE['counts'][path] = STATE['counts'].get(path, 0) + 1
            fault = STATE['fault'] if path.endswith('.deb') and not head else ''
            if fault:
                STATE['fault'] = ''
        if fault == 'fail':
            self.send_error(503)
            return
        if self.headers.get('If-None-Match') == etag:
            self.send_response(304)
            self.send_header('ETag', etag)
            self.end_headers()
            return
        code, response = 200, body
        range_header = self.headers.get('Range', '')
        if range_header.startswith('bytes=') and self.headers.get('If-Range', etag) == etag:
            try:
                start, end = range_header[6:].split('-')
                if start:
                    start = int(start)
                    end = int(end) if end else len(body) - 1
                else:
                    start, end = max(0, len(body) - int(end)), len(body) - 1
                if not 0 <= start <= end < len(body):
                    raise ValueError('invalid range')
                response, code = body[start:end + 1], 206
                with LOCK:
                    STATE['ranges'] += 1
            except ValueError:
                self.send_response(416)
                self.send_header('Content-Range', 'bytes */' + str(len(body)))
                self.send_header('Content-Length', '0')
                self.end_headers()
                return
        self.send_response(code)
        self.send_header('Content-Length', str(len(response)))
        self.send_header('ETag', etag)
        self.send_header('Cache-Control', 'public, max-age=3600')
        self.send_header('Accept-Ranges', 'bytes')
        if code == 206:
            self.send_header('Content-Range', f'bytes {start}-{end}/{len(body)}')
        self.end_headers()
        if head:
            return
        if fault == 'interrupt':
            self.wfile.write(response[:len(response) // 2])
            self.wfile.flush()
            self.connection.shutdown(socket.SHUT_RDWR)
            self.connection.close()
            return
        self.wfile.write(response)


plain = http.server.ThreadingHTTPServer(('127.0.0.1', 8080), Repository)
tls = http.server.ThreadingHTTPServer(('127.0.0.1', 8443), Repository)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain('/fixture/cert.pem', '/fixture/key.pem')
tls.socket = context.wrap_socket(tls.socket, server_side=True)
threading.Thread(target=tls.serve_forever, daemon=True).start()
plain.serve_forever()
