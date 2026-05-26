"""Generate _version.py with git short hash, then defer to pyproject.toml."""

import os
import subprocess
from pathlib import Path

from setuptools import setup

HERE = Path(__file__).resolve().parent


def _git_short_hash() -> str:
    if sha := os.environ.get("WHISPER_DICTATION_GIT_SHA"):
        return sha
    try:
        return subprocess.check_output(
            ["git", "-C", str(HERE), "rev-parse", "--short", "HEAD"],
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
    except (subprocess.CalledProcessError, FileNotFoundError):
        return "unknown"


(HERE / "whisper_dictation" / "_version.py").write_text(
    f'__version__ = "{_git_short_hash()}"\n'
)

setup()
