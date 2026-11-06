"""Fetch the current public OpenAPI schema for API inspection.

Until the public endpoint exists, use --core /path/to/avrea-core. The downloaded
schema is disposable inspection input, not an API version pin or a runtime dependency.
"""

from pathlib import Path
import argparse
import json
import os
import subprocess
import tempfile
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
DEFAULT_OUTPUT = ROOT / "build/go/openapi.json"
MAX_BYTES = 32 * 1024 * 1024


def validate(raw: bytes) -> dict:
    document = json.loads(raw)
    if not isinstance(document, dict) or document.get("openapi") != "3.1.0":
        raise ValueError("Expected an OpenAPI 3.1.0 document")
    if not isinstance(document.get("paths"), dict) or not document["paths"]:
        raise ValueError("OpenAPI document has no paths")
    return document


def fetch(
    *, core: Path | None = None, url: str | None = None, output: Path = DEFAULT_OUTPUT, core_ref: str = "origin/main"
) -> Path:
    if (core is None) == (url is None):
        raise ValueError("Choose exactly one schema source: --core or --url")
    if core is not None:
        raw = subprocess.check_output(["git", "-C", str(core), "show", core_ref + ":control/openapi/public.json"])
    else:
        assert url is not None  # The exclusive-source check above guarantees this.
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password:
            raise ValueError("Schema URL must be HTTPS without embedded credentials")
        with urllib.request.urlopen(url, timeout=60) as response:
            if urllib.parse.urlsplit(response.geturl()).scheme != "https":
                raise ValueError("Schema download redirected away from HTTPS")
            raw = response.read(MAX_BYTES + 1)
    if len(raw) > MAX_BYTES:
        raise ValueError("OpenAPI document exceeds 32 MiB")
    validate(raw)
    output.parent.mkdir(parents=True, exist_ok=True)
    # A failed download or validation never replaces a usable local input.
    with tempfile.NamedTemporaryFile(dir=output.parent, delete=False) as temporary:
        staged = Path(temporary.name)
        try:
            temporary.write(raw)
            temporary.close()
            staged.replace(output)
        finally:
            staged.unlink(missing_ok=True)
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group()
    source.add_argument("--core", type=Path)
    source.add_argument("--url", default=os.environ.get("AVR_OPENAPI_URL"))
    parser.add_argument(
        "--core-ref", default="origin/main", help="Git revision in the local core checkout (default: origin/main)"
    )
    parser.add_argument("--out", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()
    print(fetch(core=args.core, url=None if args.core else args.url, output=args.out, core_ref=args.core_ref))


if __name__ == "__main__":
    main()
