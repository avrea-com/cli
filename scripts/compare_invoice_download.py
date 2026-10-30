"""Invoice PDF parity: download bytes, destination effects, redirects and errors."""

from compression import zstd
from copy import deepcopy
from scripts import compare_artifact as artifact
from scripts import compare_auth as harness
from scripts import compare_billing as billing
from scripts import compare_inspection as inspection
import sys

INVOICE = billing.INVOICE
PATH = billing.BASE + "/invoices/" + INVOICE + "/pdf"
GET = ("GET", PATH)
PDF = b"%PDF-1.7\nfixture invoice\x00\xff\n%%EOF\n"
FILENAME = INVOICE + ".pdf"


def scenarios():
    cases = []

    def case(name, args=None, expected=None, **kwargs):
        cases.append(
            {
                "name": "invoice download " + name,
                "args": ["billing", "invoices", "download", *(args if args is not None else [INVOICE])],
                "env": billing.ENV.copy(),
                "responses": {GET: (200, PDF, {"Content-Type": "application/pdf"})},
                "capture_files": [FILENAME, "downloads", "-", ".pdf"],
                "fixture_dirs": ["downloads"],
                "expected_files": {FILENAME: artifact.file_record(PDF, 0o644)} if expected is None else expected,
                **kwargs,
            }
        )

    case("default", presentation=True)
    case(
        "explicit org",
        [INVOICE, "--org", billing.ORG],
        env=billing.ENV | {"AVR_ORG": "org-other", "AVR_REPO": "ignore/repo"},
        presentation=True,
    )
    case(
        "org slug",
        [INVOICE, "--org", "AcMe"],
        responses={GET: (200, PDF), ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
        presentation=True,
    )
    case(
        "custom",
        [INVOICE, "--out", "downloads/invoice.pdf"],
        expected={"downloads/invoice.pdf": artifact.file_record(PDF, 0o644)},
        presentation=True,
    )
    case(
        "replace",
        [INVOICE, "--out", "downloads/existing.pdf"],
        fixture_files={"downloads/existing.pdf": b"old"},
        expected={"downloads/existing.pdf": artifact.file_record(PDF, 0o644)},
        presentation=True,
    )
    case("empty out", [INVOICE, "--out", ""], presentation=True)
    case(
        "dash is filename", [INVOICE, "--out", "-"], expected={"-": artifact.file_record(PDF, 0o644)}, presentation=True
    )
    case(
        "empty body",
        responses={GET: (200, b"")},
        expected={FILENAME: artifact.file_record(b"", 0o644)},
        presentation=True,
    )
    for status in [301, 302, 303, 307, 308]:
        case(
            "redirect " + str(status),
            responses={
                GET: (status, b"", {"Location": "/invoice-file?signature=fixture"}),
                ("GET", "/invoice-file?signature=fixture"): (200, PDF),
            },
            presentation=True,
        )
    for encoding in ["gzip", "zstd"]:
        case(encoding, encoding=encoding, presentation=True)
        if encoding == "zstd":
            # Standalone Python httpx saves unsupported zstd bytes as-is.
            # Go intentionally saves the decoded PDF, as its transport supports zstd.
            cases[-1]["python_expected_files"] = {FILENAME: artifact.file_record(zstd.compress(PDF), 0o644)}
    case("no auth", env={}, responses={}, expected={}, exit=4)
    case("missing argument", [], responses={}, expected={}, exit=2, error_terms=["INVOICE_ID"])
    case("no JSON", [INVOICE, "--json", "*"], responses={}, expected={}, exit=2, error_terms=["--json"])
    for name, out in [("missing parent", "downloads/missing/file.pdf"), ("directory", "downloads")]:
        case(name, [INVOICE, "--out", out], expected={}, exit=1, error_terms=["Error writing file", "Aborted!"])
    for status in [401, 403, 404, 409, 422, 429, 503]:
        case(
            "HTTP " + str(status),
            [INVOICE, "--out", "downloads/existing.pdf"],
            responses={GET: (status, {"detail": "fixture denied"})},
            fixture_files={"downloads/existing.pdf": b"keep"},
            expected={"downloads/existing.pdf": artifact.file_record(b"keep", 0o644)},
            exit=4 if status == 401 else 1,
        )
    if sys.platform != "win32":
        for name in ["default", "custom"]:
            item = deepcopy(next(c for c in cases if c["name"] == "invoice download " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    if "python_expected_files" not in scenario:
        return artifact.compare(scenario, python, go)
    common = {key: value for key, value in scenario.items() if key != "expected_files"}
    differences = artifact.compare(common, python, go)
    if python.get("files") != scenario["python_expected_files"]:
        differences.append("Python compressed PDF file effects")
    if go.get("files") != scenario["expected_files"]:
        differences.append("Go decoded PDF file effects")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios,
            compare_results=compare,
            default_report="build/go/invoice-download-parity.json",
        )
    )
