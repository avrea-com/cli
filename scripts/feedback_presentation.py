"""Presentation-only fixture for the new Go command in the Python parity oracle.

The pinned Python CLI predates feedback. This declaration lets existing root help,
completion and docs comparisons keep testing their full output, including the new
command. It implements no submission behavior; Go HTTP tests cover that contract.
"""

import click


@click.group(invoke_without_command=True, subcommand_metavar="COMMAND [ARGS]...")
@click.argument("message", required=False)
@click.option("--bug", is_flag=True, default=False, help="Mark the feedback as a bug report.")
@click.option("--idempotency-key", default="", help="Reuse a previous submission key (32 lowercase hex characters).")
@click.option("--jq", "-q", default="", help="Filter JSON with an embedded jq expression.")
@click.option("--json", default="", help="Output comma-separated fields, * for all, or ? to list fields.")
@click.option("--org", default="", help="Attribute feedback to this organization slug or ID.")
def feedback(**kwargs: object) -> None:
    """Send MESSAGE to the Avrea team. Sign in with avr auth login first.

    Use --bug for a bug report and --org SLUG_OR_ID to attribute it to an
    organization. Without --org, feedback is personal, regardless of your default
    organization. Contact details come from your account.

    The idempotency key is printed before sending. If a request fails, repeat
    the same message, kind and organization with --idempotency-key KEY.
    Acceptance queues delivery; use avr feedback status FEEDBACK_ID to check it.
    Use -- before a message beginning with a dash or the literal word status.
    """
    raise AssertionError("Presentation fixture must never submit feedback")


feedback.short_help = "Send feedback or report a bug."
assert feedback.help is not None
feedback.help = feedback.help.rstrip()


@feedback.command(short_help="Read the delivery status of your feedback.")
@click.argument("feedback_id")
@click.option("--jq", "-q", default="", help="Filter JSON with an embedded jq expression.")
@click.option("--json", default="", help="Output comma-separated fields, * for all, or ? to list fields.")
def status(**kwargs: object) -> None:
    """Read the delivery status of your feedback."""
    raise AssertionError("Presentation fixture must never read feedback")
