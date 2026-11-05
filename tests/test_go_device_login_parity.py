"""The QR comparison must reject wrong destinations and broken presentation."""

from io import StringIO
from scripts.compare_device_login import qr_bitmap
import click
import pytest
import segno

LINK = "https://console.example.test/device?user_code=BCDF-GHJK"


def capture(encoded: str = LINK, displayed: str = LINK) -> str:
    stream = StringIO()
    segno.make_qr(encoded, error="m").terminal(out=stream, compact=True)
    rows = "\n".join(click.style(line, fg="white", bg="black") for line in stream.getvalue().splitlines())
    return (
        f"Open this link in a browser on any device:\n\n  {displayed}\n\n"
        f"Or scan with your phone:\n{rows}\n\nWaiting for approval\n"
    )


def test_qr_decodes_public_link_and_normalizes_only_symbol() -> None:
    text, pixels = qr_bitmap(capture().replace("\n", "\r\n"))
    assert LINK in text
    assert "<valid QR symbol>" in text
    assert pixels is not None


def test_different_valid_qr_destination_is_rejected() -> None:
    with pytest.raises(ValueError, match="displayed sign-in link"):
        qr_bitmap(capture(encoded="https://wrong.example.test/device"))


@pytest.mark.parametrize("damage", ["color", "quiet-zone", "row-width"])
def test_broken_terminal_qr_is_rejected(damage: str) -> None:
    text = capture()
    if damage == "color":
        text = text.replace("\x1b[37m", "\x1b[30m", 1)
    elif damage == "quiet-zone":
        text = text.replace("█", " ", 1)
    else:
        text = text.replace("█", "", 1)
    with pytest.raises(ValueError):
        qr_bitmap(text)
