"""Temporary, control-segment-only package proxy for the clean NAC node.

Run on EVE after moving 10.70.99.254/24 to the candidate control bridge.
This is an application proxy; it never enables IP forwarding. Only the
candidate control node may connect, and only the listed package hosts are
reachable. Stop the process after installation.
"""

import argparse
import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import select
import socket
from urllib.parse import urlsplit


CLIENT = "10.70.99.2"
LISTEN = "10.70.99.254"
ALLOWED = {
    "archive.ubuntu.com",
    "security.ubuntu.com",
    "ports.ubuntu.com",
    "proxy.golang.org",
    "sum.golang.org",
}


def allowed(host):
    host = host.lower().rstrip(".")
    return host in ALLOWED or host.endswith(".archive.ubuntu.com")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format_, *args):
        super().log_message(format_, *args)

    def authorized(self):
        if self.client_address[0] == CLIENT:
            return True
        self.send_error(403, "candidate control node only")
        return False

    def do_CONNECT(self):
        if not self.authorized():
            return
        host, sep, port_text = self.path.rpartition(":")
        if not sep or port_text != "443" or not allowed(host):
            self.send_error(403, "destination not in package allowlist")
            return
        try:
            remote = socket.create_connection((host, 443), timeout=10)
        except OSError:
            self.send_error(502, "upstream unavailable")
            return
        with remote:
            self.send_response(200, "Connection Established")
            self.end_headers()
            self.connection.settimeout(60)
            remote.settimeout(60)
            sockets = (self.connection, remote)
            while True:
                readable, _, _ = select.select(sockets, [], [], 60)
                if not readable:
                    break
                for source in readable:
                    try:
                        data = source.recv(65536)
                    except (BlockingIOError, OSError):
                        return
                    if not data:
                        return
                    target = remote if source is self.connection else self.connection
                    try:
                        target.sendall(data)
                    except OSError:
                        return

    def do_HEAD(self):
        self.forward_http()

    def do_GET(self):
        self.forward_http()

    def forward_http(self):
        if not self.authorized():
            return
        url = urlsplit(self.path)
        if url.scheme != "http" or not url.hostname or not allowed(url.hostname) or url.port not in (None, 80):
            self.send_error(403, "destination not in package allowlist")
            return
        path = url.path or "/"
        if url.query:
            path += "?" + url.query
        try:
            upstream = http.client.HTTPConnection(url.hostname, 80, timeout=30)
            upstream.request(self.command, path, headers={"User-Agent": "nac-lab-package-proxy/1", "Host": url.hostname})
            response = upstream.getresponse()
            self.send_response(response.status, response.reason)
            for name, value in response.getheaders():
                if name.lower() not in {"connection", "transfer-encoding", "keep-alive", "proxy-connection"}:
                    self.send_header(name, value)
            self.send_header("Connection", "close")
            self.end_headers()
            if self.command != "HEAD":
                while data := response.read(65536):
                    self.wfile.write(data)
            upstream.close()
        except (OSError, http.client.HTTPException):
            self.close_connection = True
            self.send_error(502, "upstream unavailable")
        self.close_connection = True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=3128)
    args = parser.parse_args()
    if not 1024 <= args.port <= 65535:
        parser.error("port must be 1024..65535")
    ThreadingHTTPServer((LISTEN, args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
