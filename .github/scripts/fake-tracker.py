#!/usr/bin/env python3
"""Minimal HTTPS fake tracker for TrackerClient e2e tests (stdlib only).

Endpoints (mirrors README Tracker API used by internal/client/tracker_client.go):
  GET  /tracker/nodes/seeds?count=&exclude=&include_cert=true -> {"seeds":[],"total_known":0}
  GET  /tracker/geo                                           -> {"region":"e2e-region"}
  GET  /tracker/crl                                           -> {"version":1,"revoked_ids":[]}
  POST /tracker/nodes                                         -> {"region":"e2e-region"}
  POST /tracker/nodes/{id}/heartbeat                          -> {}
  DELETE /tracker/nodes/{id}                                  -> {}

Every request is appended to --log as: METHOD PATH body=<truncated-body>
"""
import argparse
import http.server
import json
import ssl
import urllib.parse


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, default=18650)
    p.add_argument("--cert", required=True)
    p.add_argument("--key", required=True)
    p.add_argument("--log", required=True)
    return p.parse_args()


def make_handler(log_path):
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):  # silence default stderr logging
            pass

        def _append_log(self, body=b""):
            try:
                with open(log_path, "a", encoding="utf-8") as f:
                    snippet = body[:500].decode("utf-8", errors="replace")
                    f.write(f"{self.command} {self.path} body={snippet}\n")
            except OSError:
                pass

        def _send(self, code, obj):
            data = json.dumps(obj).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            self._append_log()
            parsed = urllib.parse.urlparse(self.path)
            if parsed.path == "/tracker/nodes/seeds":
                self._send(200, {"seeds": [], "total_known": 0, "note": "fake"})
            elif parsed.path == "/tracker/geo":
                self._send(200, {"region": "e2e-region"})
            elif parsed.path == "/tracker/crl":
                self._send(200, {"version": 1, "revoked_ids": []})
            else:
                self._send(404, {"error": {"code": "NOT_FOUND", "message": "not found"}})

        def do_POST(self):
            length = int(self.headers.get("Content-Length", 0) or 0)
            body = self.rfile.read(length) if length else b""
            self._append_log(body)
            if self.path.startswith("/tracker/nodes/") and self.path.endswith("/heartbeat"):
                self._send(200, {})
            elif self.path == "/tracker/nodes":
                self._send(200, {"region": "e2e-region"})
            else:
                self._send(404, {"error": {"code": "NOT_FOUND", "message": "not found"}})

        def do_DELETE(self):
            self._append_log()
            if self.path.startswith("/tracker/nodes/"):
                self._send(200, {})
            else:
                self._send(404, {"error": {"code": "NOT_FOUND", "message": "not found"}})

    return Handler


def main():
    args = parse_args()
    # Truncate log on startup so each run is hermetic.
    with open(args.log, "w", encoding="utf-8"):
        pass
    server = http.server.HTTPServer(("127.0.0.1", args.port), make_handler(args.log))
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(args.cert, args.key)
    server.socket = ctx.wrap_socket(server.socket, server_side=True)
    print(f"fake tracker listening on 127.0.0.1:{args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
