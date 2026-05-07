"""Mock voice I/O.

Real implementation uses OpenAI Realtime over a WebSocket: audio in over
the mic, transcription deltas, audio out from the model. For modelling we
collapse all of that to ``listen() -> str`` and ``speak(text)``, plus a
side-channel ``cleanup`` step that approximates what a transcription
post-processor (or the Realtime model itself) does to filler-y speech.

The cleanup step is split out as its own function so we can test
"exactly what was intended" requirements from the design sketch.
"""

from __future__ import annotations

import re
from collections import deque
from dataclasses import dataclass, field
from typing import Iterable


# Phrases removed entirely (multi-word filler).
_FILLER_PHRASES = (
    re.compile(r"\b(you know|i mean|sort of|kind of|or so)\b", re.IGNORECASE),
    re.compile(r"\b(uh+|um+|er+|ah+)\b", re.IGNORECASE),
)
# "like" is filler in two narrow cases: between commas ("um, like, hi") and
# as an approximator before a number/quantity ("like ten minutes"). "I like
# cake" stays as content.
_FILLER_LIKE = re.compile(r"(?<=[,\s])like(?=\s*[,])", re.IGNORECASE)
_NUMBER_WORDS = (
    r"\d+|a|an|one|two|three|four|five|six|seven|eight|nine|ten|"
    r"eleven|twelve|twenty|thirty|forty|fifty|sixty|seventy|eighty|"
    r"ninety|hundred|thousand|million"
)
_FILLER_LIKE_APPROX = re.compile(
    rf"\blike\s+(?=(?:{_NUMBER_WORDS})\b)", re.IGNORECASE
)


def cleanup_transcript(raw: str) -> str:
    """Strip disfluencies, normalize whitespace, fix a few obvious things.

    Conservative on purpose -- we don't paraphrase, we just remove fillers
    and tidy spacing/punctuation. The goal is "clean grammar, exactly what
    was intended" without rewriting meaning.
    """
    if not raw:
        return ""
    text = raw
    for pat in _FILLER_PHRASES:
        text = pat.sub("", text)
    text = _FILLER_LIKE.sub("", text)
    text = _FILLER_LIKE_APPROX.sub("", text)
    # Collapse repeated commas/whitespace left behind by removals.
    text = re.sub(r",\s*,+", ",", text)
    text = re.sub(r"\s+([,.!?])", r"\1", text)
    text = re.sub(r"\s{2,}", " ", text).strip(" ,.")
    if not text:
        return ""
    # Capitalize the first letter and ensure terminal punctuation.
    text = text[0].upper() + text[1:]
    if text[-1] not in ".!?":
        text += "."
    return text


@dataclass
class VoiceLayer:
    """Queue-driven mock of OpenAI Realtime.

    A test (or a simulated user) calls ``user_says(raw)`` to enqueue an
    utterance the orchestrator will pick up via ``listen()``. Anything the
    assistant says lands in ``spoken``.
    """

    pending: deque[str] = field(default_factory=deque)
    spoken: list[str] = field(default_factory=list)
    raw_transcripts: list[str] = field(default_factory=list)

    # Test / user side -----------------------------------------------------

    def user_says(self, raw: str) -> None:
        self.pending.append(raw)

    def queue_user_turns(self, turns: Iterable[str]) -> None:
        for t in turns:
            self.user_says(t)

    # Orchestrator side ----------------------------------------------------

    def listen(self) -> tuple[str, str] | None:
        """Return (cleaned_text, raw_text) or None if nothing pending."""
        if not self.pending:
            return None
        raw = self.pending.popleft()
        self.raw_transcripts.append(raw)
        return cleanup_transcript(raw), raw

    def speak(self, text: str) -> None:
        self.spoken.append(text)

    def has_pending(self) -> bool:
        return bool(self.pending)
