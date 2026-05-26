"""Generate _version.py with commit hash and a content fingerprint, then defer to pyproject.toml.

The content fingerprint is a SHA256 over the package's .py files + pyproject.toml.
It can be computed from any source tree (no .git required), so it's the
authoritative identifier of *what code is actually deployed*. The commit hash
is a label provided externally (env var or .git) which can be stale or wrong;
the content hash cannot.
"""

import hashlib
import os
import subprocess
import sys
from pathlib import Path

from setuptools import setup

HERE = Path(__file__).resolve().parent
PKG = HERE / "whisper_dictation"
VERSION_FILE = PKG / "_version.py"


def _detect_commit() -> tuple[str, str]:
    """Return (commit, source). source is 'env', 'git', 'git-dirty', or 'unknown'."""
    if sha := os.environ.get("WHISPER_DICTATION_GIT_SHA"):
        return sha, "env"
    try:
        rev = subprocess.check_output(
            ["git", "-C", str(HERE), "rev-parse", "--short", "HEAD"],
            stderr=subprocess.DEVNULL,
            text=True,
        ).strip()
        dirty = subprocess.call(
            ["git", "-C", str(HERE), "diff", "--quiet", "HEAD", "--"],
            stderr=subprocess.DEVNULL,
        )
        return (f"{rev}-dirty", "git-dirty") if dirty else (rev, "git")
    except (subprocess.CalledProcessError, FileNotFoundError):
        return "unknown", "unknown"


def _content_hash() -> str:
    """SHA256 over sorted (relpath, content) for .py files + pyproject.toml.

    Excludes _version.py itself so the hash is stable across rebuilds.
    """
    h = hashlib.sha256()
    files = [p for p in PKG.rglob("*.py") if p.name != "_version.py"]
    files.append(HERE / "pyproject.toml")
    for path in sorted(files):
        h.update(path.relative_to(HERE).as_posix().encode())
        h.update(b"\0")
        h.update(path.read_bytes())
        h.update(b"\0")
    return h.hexdigest()[:16]


commit, source = _detect_commit()
content = _content_hash()

VERSION_FILE.write_text(
    f"__commit__ = {commit!r}\n"
    f"__content_hash__ = {content!r}\n"
    f"__commit_source__ = {source!r}\n"
)

if source == "unknown":
    print(
        "whisper-dictation: WARNING — building without commit info. "
        "Set WHISPER_DICTATION_GIT_SHA to bake in a rev "
        f"(content hash = {content}).",
        file=sys.stderr,
    )

setup()
