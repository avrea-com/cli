"""Small, explicit RFC 6455 responses for the paired CLI loopback harness."""

from websockets.frames import Frame
from websockets.frames import Opcode
import base64
import hashlib
import json
import struct
import time


def respond(handler, events):
    """Upgrade an already-recorded fixture request, then emit the declared frames."""
    key = handler.headers.get("Sec-WebSocket-Key", "")
    if handler.headers.get("Upgrade", "").lower() != "websocket" or not key:
        handler.send_error(400, "Expected WebSocket upgrade")
        return
    accept = base64.b64encode(
        hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode(), usedforsecurity=False).digest()
    ).decode()
    handler.close_connection = True
    try:
        handler.wfile.write(
            (
                "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                f"Sec-WebSocket-Accept: {accept}\r\n\r\n"
            ).encode()
        )
        for event in events:
            if "delay" in event:
                time.sleep(event["delay"])
                continue
            if "disconnect" in event:
                return
            if "close" in event:
                close = event["close"]
                data = struct.pack("!H", close.get("code", 1000)) + close.get("reason", "").encode()
                handler.wfile.write(Frame(Opcode.CLOSE, data).serialize(mask=False))
                return
            raw = event.get("raw")
            if raw is None:
                raw = json.dumps(event["frame"]).encode()
            elif isinstance(raw, str):
                raw = raw.encode()
            handler.wfile.write(Frame(Opcode.TEXT, raw).serialize(mask=False))
        handler.wfile.write(Frame(Opcode.CLOSE, struct.pack("!H", 1000)).serialize(mask=False))
    except OSError:
        # A completed, canceled, or rejected command may close before the fixture.
        return
