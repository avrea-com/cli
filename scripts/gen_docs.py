"""Thin shim for ``avr internal docs``.

The real generator lives in :mod:`avrea_cli.docs_gen` so the published
``avr`` binary can render its own docs (``avr internal docs ...``). This
script exists for build environments where the entry-point script isn't
on PATH yet — ``python -m avrea_cli.docs_gen`` works equivalently.
"""

from avrea_cli.docs_gen import main

if __name__ == "__main__":
    main()
