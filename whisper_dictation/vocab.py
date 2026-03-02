#!/usr/bin/env python3
"""Compare dictation history against Claude Code history to find vocabulary hints.

Matches whisper transcriptions to the messages actually sent, identifies
corrections the user made, and suggests words for initial_prompt.
"""

import json
import sys
from collections import Counter
from datetime import datetime, timezone
from difflib import SequenceMatcher
from pathlib import Path


def load_dictations(path: Path) -> list[dict]:
    entries = []
    for line in path.read_text().splitlines():
        if not line.strip():
            continue
        d = json.loads(line)
        dt = datetime.fromisoformat(d["ts"])
        d["epoch_ms"] = int(dt.timestamp() * 1000)
        entries.append(d)
    return entries


def load_claude_messages(history_dir: Path) -> list[dict]:
    """Load all Claude Code history entries."""
    path = history_dir / "history.jsonl"
    if not path.exists():
        return []
    entries = []
    for line in path.read_text().splitlines():
        if not line.strip():
            continue
        entries.append(json.loads(line))
    return entries


def match_dictations_to_messages(dictations, messages, window_ms=120_000):
    """Group dictation entries to the Claude message they were part of.

    A dictation entry belongs to a message if:
    - The dictation timestamp is before the message timestamp
    - The gap is within window_ms
    - The dictation text appears (possibly corrected) in the message
    """
    pairs = []  # (combined_dictation_text, sent_text)
    used = set()

    for msg in messages:
        msg_ts = msg["timestamp"]
        display = msg["display"].strip()
        # Skip messages that are just pasted content references
        if display.startswith("[Pasted text"):
            continue

        # Strip pasted content references from the display text
        clean = display
        while "[Pasted text #" in clean:
            start = clean.index("[Pasted text #")
            end = clean.index("]", start) + 1
            clean = clean[:start] + clean[end:]
        clean = clean.strip()
        if not clean:
            continue

        # Find dictation entries in the time window before this message
        candidates = []
        for i, d in enumerate(dictations):
            if i in used:
                continue
            delta = msg_ts - d["epoch_ms"]
            if 0 <= delta <= window_ms:
                candidates.append((i, d))

        if not candidates:
            continue

        # Check which candidates' text appears in the sent message
        matched = []
        for i, d in candidates:
            # Use fuzzy matching - does a significant portion of the dictation
            # text appear in the message?
            ratio = SequenceMatcher(None, d["text"].lower(), clean.lower()).ratio()
            if ratio > 0.3:
                matched.append((i, d))

        if matched:
            combined = " ".join(d["text"] for _, d in matched)
            pairs.append((combined, clean))
            for i, _ in matched:
                used.add(i)

    return pairs


def find_corrections(dictated: str, sent: str) -> list[tuple[str, str]]:
    """Find word-level corrections between dictated and sent text."""
    d_words = dictated.split()
    s_words = sent.split()
    matcher = SequenceMatcher(None, d_words, s_words)
    corrections = []
    for op, i1, i2, j1, j2 in matcher.get_opcodes():
        if op == "replace":
            old = " ".join(d_words[i1:i2])
            new = " ".join(s_words[j1:j2])
            if old.lower() != new.lower():  # skip case-only diffs
                corrections.append((old, new))
    return corrections


def main():
    import tomllib

    config_path = Path.home() / ".config" / "whisper-dictation" / "config.toml"
    try:
        config = tomllib.loads(config_path.read_text())
    except (FileNotFoundError, tomllib.TOMLDecodeError):
        config = {}

    history_file = config.get("history_file")
    if not history_file:
        print("No history_file set in config. Set it in:")
        print(f"  {config_path}")
        sys.exit(1)

    history_path = Path(history_file).expanduser()
    if not history_path.exists():
        print(f"No dictation history at {history_path}")
        sys.exit(1)

    claude_history = Path.home() / ".claude" / "history.jsonl"
    if not claude_history.exists():
        print(f"No Claude Code history at {claude_history}")
        sys.exit(1)

    dictations = load_dictations(history_path)
    messages = load_claude_messages(claude_history.parent)

    pairs = match_dictations_to_messages(dictations, messages)

    if not pairs:
        print("No matched dictation/message pairs found.")
        sys.exit(0)

    all_corrections = []
    print(f"Found {len(pairs)} matched pairs:\n")
    for dictated, sent in pairs:
        corrections = find_corrections(dictated, sent)
        if corrections:
            print(f"  WHISPER: {dictated}")
            print(f"  SENT:    {sent}")
            for old, new in corrections:
                print(f"    {old!r} -> {new!r}")
                all_corrections.append((old, new))
            print()

    if all_corrections:
        # Count correction targets as vocabulary hints
        hints = Counter(new for _, new in all_corrections)
        print("Suggested vocabulary hints:")
        for word, count in hints.most_common():
            print(f"  {word} (corrected {count}x)")
        print()
        hint_str = ", ".join(word for word, _ in hints.most_common())
        current = config.get("vocabulary_hints", "")
        if current:
            print(f"Current vocabulary_hints: {current!r}")
        print(f"Suggested additions: {hint_str!r}")
    else:
        print("No corrections found yet. Keep dictating!")
