"""Loopback protocol fixtures used by the service smoke tests."""
import concurrent.futures
import hashlib
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import grpc
from graphql import build_schema, graphql_sync
from websockets.sync.client import connect
from websockets.sync.server import serve

SCHEMA = build_schema("type Query { echo(value: String!): String! }")
SCHEMA.query_type.fields["echo"].resolve = lambda _, info, value: value


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def reply(self, body, content_type="text/plain"):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/events":
            events = [b"data: first\n\n", b"data: second\n\n"]
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(sum(map(len, events))))
            self.end_headers()
            for event in events:
                self.wfile.write(event)
                self.wfile.flush()
                time.sleep(2)
        elif self.path == "/blob":
            self.reply(b"x" * (16 * 1024**2), "application/octet-stream")
        else:
            self.reply(b"homebox-smoke")

    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"]))
        if self.path == "/graphql":
            request = json.loads(body)
            result = graphql_sync(SCHEMA, request["query"])
            self.reply(json.dumps(result.formatted).encode(), "application/json")
        else:
            self.reply(hashlib.sha256(body).hexdigest().encode())


def websocket(connection):
    for message in connection:
        connection.send(message)


def grpc_probe():
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
        server = grpc.server(executor)
        server.add_generic_rpc_handlers((grpc.method_handlers_generic_handler(
            "smoke.Echo", {"Echo": grpc.unary_unary_rpc_method_handler(lambda data, _: data)}
        ),))
        port = server.add_insecure_port("127.0.0.1:0")
        server.start()
        try:
            with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
                assert channel.unary_unary("/smoke.Echo/Echo")(b"smoke", timeout=5) == b"smoke"
        finally:
            server.stop(0).wait(5)


if sys.argv[1] in ("http", "ws"):
    bind_host, bind_port = sys.argv[2:] if len(sys.argv) > 2 else (
        "127.0.0.1", 18100 if sys.argv[1] == "http" else 18101
    )

if sys.argv[1] == "http":
    with ThreadingHTTPServer((bind_host, int(bind_port)), Handler) as server:
        server.serve_forever()
elif sys.argv[1] == "ws":
    with serve(websocket, bind_host, int(bind_port)) as server:
        server.serve_forever()
elif sys.argv[1] == "ws-client":
    with connect(sys.argv[2], open_timeout=5, close_timeout=2) as connection:
        for index, message in enumerate(("hello", "still here")):
            connection.send(message)
            assert connection.recv(timeout=5) == message
            if index == 0:
                time.sleep(10)
elif sys.argv[1] == "grpc":
    grpc_probe()
