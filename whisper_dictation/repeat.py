#!/usr/bin/env python3
"""Re-type the last dictation."""

import sys

LAST_TEXT_FILE = "/tmp/whisper-dictation-last"


def main():
    try:
        with open(LAST_TEXT_FILE) as f:
            text = f.read()
    except FileNotFoundError:
        sys.exit(0)

    if not text:
        sys.exit(0)

    from .text_output import type_text
    type_text(text)


if __name__ == "__main__":
    main()
