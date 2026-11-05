"""Device-login executable parity with isolated credentials and loopback OAuth."""

from copy import deepcopy
from PIL import Image
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
import re
import sys
import zxingcpp

SECRET = "private-device-secret"
KEY = "avk_" + "a" * 43
DISCOVERY = ("GET", "/.well-known/oauth-authorization-server")
START = ("POST", "/oauth/device_authorization")
POLL = ("POST", "/oauth/token")
METADATA = {
    "issuer": "FIXTURE_HOST",
    "device_authorization_endpoint": "FIXTURE_HOST/oauth/device_authorization",
    "token_endpoint": "FIXTURE_HOST/oauth/token",
}
REQUEST = {
    "device_code": SECRET,
    "user_code": "BCDF-GHJK",
    "verification_uri": "https://console.example.test/device",
    "verification_uri_complete": "https://console.example.test/device?user_code=BCDF-GHJK",
    "expires_in": 60,
    "interval": 1,
}
TOKEN = {"access_token": KEY, "token_type": "Bearer", "expires_in": 7776000, "scope": "avrea"}
BASE = {
    DISCOVERY: (200, METADATA),
    START: (200, REQUEST),
    POLL: (200, TOKEN),
    ("GET", "/users/me"): (200, harness.USER),
    ("GET", "/users/me/organizations"): (200, {"data": [{"organization_id": "org-fixture", "slug": "fixture"}]}),
}


def scenarios():
    cases = []

    def add(name, **kwargs):
        cases.append(
            {
                "name": "device " + name,
                "args": ["auth", "device-login", "--no-qr"],
                "expand_host": True,
                "browser_capture": True,
                "responses": BASE,
                "writes": True,
                **kwargs,
            }
        )

    add("approved")
    add("manual code", responses=BASE | {START: (200, REQUEST | {"verification_uri_complete": None})})
    add("pending", responses=BASE | {POLL: [(400, {"error": "authorization_pending"}), (200, TOKEN)]})
    add("backoff", responses=BASE | {POLL: [(503, {}, {"Retry-After": "2"}), (200, TOKEN)]})
    add("transport retry", responses=BASE | {POLL: [(0, {}), (200, TOKEN)]})
    add("expired before poll", responses=BASE | {START: (200, REQUEST | {"expires_in": 1, "interval": 5})}, exit=1)
    add("no profile", responses=BASE | {("GET", "/users/me"): (503, {})})
    add(
        "multiple orgs",
        responses=BASE
        | {
            ("GET", "/users/me/organizations"): (
                200,
                {"data": [{"organization_id": "org-one"}, {"organization_id": "org-two"}]},
            )
        },
    )
    add(
        "existing org",
        hosts={
            "default_host": "FIXTURE_HOST",
            "hosts": {"FIXTURE_HOST": {"token": "old-secret", "default_org": "org-existing"}},
        },
    )
    add("quiet pipe hides QR", args=["auth", "device-login"])
    for endpoint in [DISCOVERY, START, POLL]:
        for status in [302, 400, 404, 405, 501]:
            add(
                f"{endpoint[1]} HTTP {status}",
                responses=BASE
                | {endpoint: (status, {"error": "invalid_request"}, {"Location": "https://evil.example.test/"})},
                exit=1,
            )
    for error in [
        "access_denied",
        "expired_token",
        "invalid_grant",
        "invalid_client",
        "invalid_scope",
        "unsupported_grant_type",
        "unknown",
    ]:
        add("rejected " + error, responses=BASE | {POLL: (400, {"error": error, "error_description": SECRET})}, exit=1)
    for target, original, changes in [
        (
            DISCOVERY,
            METADATA,
            [
                {"issuer": "https://evil.test"},
                {"issuer": None},
                {"token_endpoint": "https://evil.test/token"},
                {"device_authorization_endpoint": "http://evil.test/device"},
            ],
        ),
        (
            START,
            REQUEST,
            [
                {"device_code": ""},
                {"user_code": "evil\x1b[31m"},
                {"expires_in": True},
                {"expires_in": 86401},
                {"interval": "1"},
                {"verification_uri": "http://evil.test/device"},
                {"verification_uri_complete": "https://evil.test/device"},
                {"verification_uri_complete": 42},
            ],
        ),
        (
            POLL,
            TOKEN,
            [
                {"access_token": SECRET},
                {"scope": "other"},
                {"token_type": "Basic"},
                {"expires_in": "60"},
                {"expires_in": False},
            ],
        ),
    ]:
        for i, change in enumerate(changes):
            add(f"invalid {target[1]} {i}", responses=BASE | {target: (200, original | change)}, exit=1)
    for endpoint in [DISCOVERY, START, POLL]:
        add("malformed " + endpoint[1], responses=BASE | {endpoint: (200, b'{"secret":')}, exit=1)
    add(
        "unsafe configured host",
        env={"AVR_HOST": "http://evil.test"},
        exit=1,
        error_terms=["plain-http remote host would send the API key in cleartext", "Use https://"],
    )
    if sys.platform != "win32":
        for original in cases[:2]:
            cases.append(
                original
                | {
                    "name": original["name"] + " terminal",
                    "tty": True,
                    "presentation": True,
                    "env": {"NO_COLOR": "", "TERM": "xterm-256color"},
                }
            )
        add("interrupt", interrupt_after_text="Waiting for approval", exit=130)
        for name, env, visible in [
            ("QR", {"COLUMNS": "100"}, True),
            ("narrow QR", {"COLUMNS": "20"}, False),
            ("dumb QR", {"TERM": "dumb"}, False),
            ("no color QR", {"NO_COLOR": "1"}, False),
        ]:
            add(
                name,
                args=["auth", "device-login"],
                tty=True,
                presentation=True,
                env={"NO_COLOR": "", "TERM": "xterm-256color"} | env,
                qr=visible,
            )
    return cases


def qr_bitmap(text):
    """Decode compact terminal cells; preserve encoder-specific module choices."""
    text = text.replace("\r\n", "\n")
    before, label, after = text.partition("Or scan with your phone:\n")
    if not label:
        return text, None
    rows, rest = after.split("\n\n", 1)
    pixels = []
    modules = {"█": (False, False), "▀": (False, True), "▄": (True, False), " ": (True, True)}
    for raw in rows.splitlines():
        if not raw.startswith("\x1b[37m\x1b[40m") or not raw.endswith("\x1b[0m"):
            raise ValueError("QR colors must be explicit white on black")
        line = re.sub(r"\x1b\[[0-9;]*m", "", raw)
        cells = [modules[cell] for cell in line]
        pixels.extend([[cell[i] for cell in cells] for i in range(2)])
    size = len(pixels[0])
    # QR symbols have an odd width; compact output pads below the quiet zone.
    if len(pixels) != size + 1 or any(len(row) != size for row in pixels) or not all(pixels[-1]):
        raise ValueError("QR shape or final row is invalid")
    pixels.pop()
    if any(
        value
        for y, row in enumerate(pixels)
        for x, value in enumerate(row)
        if x < 4 or y < 4 or x >= size - 4 or y >= size - 4
    ):
        raise ValueError("QR quiet zone is not light")
    for left, top in [(4, 4), (size - 11, 4), (4, size - 11)]:
        for y in range(7):
            for x in range(7):
                dark = x in (0, 6) or y in (0, 6) or (2 <= x <= 4 and 2 <= y <= 4)
                if pixels[top + y][left + x] != dark:
                    raise ValueError("QR finder pattern is damaged")
    image = Image.frombytes("L", (size, size), bytes(0 if pixel else 255 for row in pixels for pixel in row))
    decoded = zxingcpp.read_barcodes(image.resize((size * 4, size * 4), Image.Resampling.NEAREST))
    expected = before.split("Open this link in a browser on any device:\n\n  ", 1)[1].splitlines()[0]
    if len(decoded) != 1 or decoded[0].text != expected:
        raise ValueError("QR must decode to the displayed sign-in link")
    return before + label + "<valid QR symbol>\n\n" + rest, pixels


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    for label, result in (("Python", python), ("Go", go)):
        for secret in [SECRET, KEY, "old-secret"]:
            if secret in result["stdout"] + result["stderr"]:
                differences.append(label + " leaked credential")
        for request in result["requests"]:
            if request["path"].startswith(("/oauth/", "/.well-known/")) and (
                request["authorization"] or request["cookie"]
            ):
                differences.append(label + " authenticated device authorization request")
        if result["browser"]:
            differences.append(label + " opened local browser")
        if "qr" in scenario:
            try:
                result["stdout"], pixels = qr_bitmap(result["stdout"])
                if (pixels is not None) != scenario["qr"]:
                    differences.append(label + " incorrect QR visibility")
            except (ValueError, KeyError, IndexError) as exc:
                differences.append(label + " invalid terminal QR: " + str(exc))
    # Endpoint host expansion and temporary credentials paths are normalized by
    # the shared harness; compare every request, saved field, and visible word.
    return differences + inspection.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/device-login-parity.json"
        )
    )
