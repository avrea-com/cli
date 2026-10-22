"""``avr vm create --from-spawn-script``: an agent sandbox for a vendor's work order.

A coding-agent vendor's controller runs a spawn script on the customer's host
for each queued session, with the work order in its environment. Nothing
connects in: the controller polls the vendor and runs the script locally.
This mode reads the work order, creates one Avrea agent sandbox for it with
the order's id as the idempotency key, and exits with the code the vendor's
controller understands.

Only Claude Code's self-hosted runner orchestrator is supported. Its
controller backs off and offers the session again when a spawn fails for a
retryable reason. Cursor's controller documents no retry, so a spawn refused
for quota would orphan the worker id it claimed. Cursor waits until a refused
spawn can queue instead.

It reads only the per-session credential, never the vendor's environment
key, and never prints or logs the credential.
"""

from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any

# The CLI's names for the API's bootstrap providers.
SPAWN_PROVIDERS: dict[str, str] = {
    "claude-code": "claude_code",
}

# Claude Code's spawn-runner contract: 0 submitted, 1 retryable (the session
# backs off and is offered again), 2 and above blocked until an owner
# retries.
EXIT_CREATED = 0
EXIT_RETRY = 1
EXIT_REFUSED = 2

# The size a sandbox gets unless --size says otherwise: room for a build
# beside Anthropic's 4 GiB, 2-4 CPU guidance for the runner.
DEFAULT_SANDBOX_SIZE = "4-vcpu"


class SpawnInputError(Exception):
    """The vendor's environment does not carry a usable work order."""


@dataclass(frozen=True, slots=True)
class SpawnOrder:
    """One work order: the key that makes its create idempotent, and the
    bootstrap block the create sends."""

    idempotency_key: str
    bootstrap: dict[str, Any]


def _required(environ: Mapping[str, str], name: str) -> str:
    value = environ.get(name, "").strip()
    if not value:
        raise SpawnInputError(f"{name} is not set; run this as the vendor controller's spawn script")
    return value


def read_spawn_order(provider: str, environ: Mapping[str, str]) -> SpawnOrder:
    """The work order ``provider``'s controller handed this process."""
    match provider:
        case "claude-code":
            order_id = _required(environ, "CLAUDE_RUNNER_ORDER_ID")
            path = _required(environ, "CLAUDE_RUNNER_WORK_ORDER_FILE")
            # The orchestrator deletes the file once this process exits, so it
            # is read now and travels in the create request.
            try:
                credential = Path(path).read_text(encoding="utf-8").strip()
            except OSError as exc:
                raise SpawnInputError(f"cannot read the work order file: {exc.strerror}") from None
            if not credential:
                raise SpawnInputError("the work order file is empty")
            return SpawnOrder(order_id, {"provider": SPAWN_PROVIDERS[provider], "credential": credential})
        case _:
            raise NotImplementedError(f"no spawn script support for {provider}")


def exit_code_for_status(status: int) -> int:
    """The vendor exit code for the create's HTTP status. A timeout, a
    conflict (a quota), a rate limit and a server error can pass, so the
    vendor offers the session again; any other refusal needs someone to act."""
    if 200 <= status < 300:
        return EXIT_CREATED
    if status in (408, 409, 429) or status >= 500:
        return EXIT_RETRY
    return EXIT_REFUSED
