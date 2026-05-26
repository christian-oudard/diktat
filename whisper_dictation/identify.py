"""Find the commit whose source tree matches the embedded content hash.

Run from inside a whisper_dictation clone:

    $ whisper-dictation-daemon --identify-commit

Walks every commit on every branch and recomputes the content hash from the
tree at that commit, using the same algorithm as setup.py. Prints commits that
match. If no commit matches, the deployed build is from uncommitted code.
"""

import hashlib
import subprocess
import sys
from pathlib import Path

from . import __content_hash__


def _hash_tree(rev: str, root: Path) -> str:
    """Recompute setup.py's content hash from the git tree at rev."""
    listing = subprocess.check_output(
        ["git", "-C", str(root), "ls-tree", "-r", "-z", rev,
         "whisper_dictation/", "pyproject.toml"],
        text=True,
    )
    entries = []
    for entry in listing.split("\0"):
        if not entry:
            continue
        meta, path = entry.split("\t", 1)
        _, _, blob = meta.split()
        if path.startswith("whisper_dictation/") and not path.endswith(".py"):
            continue
        if path.endswith("_version.py"):
            continue
        entries.append((path, blob))
    entries.sort()
    h = hashlib.sha256()
    for path, blob in entries:
        content = subprocess.check_output(
            ["git", "-C", str(root), "cat-file", "blob", blob],
        )
        h.update(path.encode())
        h.update(b"\0")
        h.update(content)
        h.update(b"\0")
    return h.hexdigest()[:16]


def identify_commit():
    root = Path.cwd()
    if not (root / ".git").is_dir():
        sys.exit(f"not a git repository: {root} — cd into a whisper_dictation clone")

    if __content_hash__ == "unknown":
        sys.exit("no content hash embedded in this build")

    print(f"looking for commit with content hash {__content_hash__}")
    revs = subprocess.check_output(
        ["git", "-C", str(root), "log", "--all", "--format=%H %h %s"],
        text=True,
    ).splitlines()

    matches = []
    for line in revs:
        full, short, *subject = line.split(maxsplit=2)
        try:
            tree_hash = _hash_tree(full, root)
        except subprocess.CalledProcessError:
            continue
        if tree_hash == __content_hash__:
            matches.append((short, subject[0] if subject else ""))

    if not matches:
        print("no matching commit. Deployed build is from uncommitted source.")
        sys.exit(1)
    for short, subject in matches:
        print(f"  {short}  {subject}")
