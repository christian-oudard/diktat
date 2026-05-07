"""Shared types that flow between the voice layer, translator, executor,
and MCP servers.

Kept deliberately small and immutable. The whole point is to nail down the
shapes that cross component boundaries so each component can be swapped.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal, Union

Speaker = Literal["user", "assistant"]


@dataclass(frozen=True)
class Utterance:
    speaker: Speaker
    text: str
    raw_text: str | None = None  # pre-cleanup transcript, if known
    timestamp: float = 0.0


@dataclass
class Conversation:
    """Ordered list of turns. Owned by the translator; passed wholesale to
    the executor (per the design sketch's simplest protocol)."""

    utterances: list[Utterance] = field(default_factory=list)

    def append(self, u: Utterance) -> None:
        self.utterances.append(u)

    def last_user(self) -> Utterance | None:
        for u in reversed(self.utterances):
            if u.speaker == "user":
                return u
        return None

    def transcript(self) -> str:
        return "\n".join(f"{u.speaker}: {u.text}" for u in self.utterances)


@dataclass(frozen=True)
class ToolSpec:
    """What the executor advertises to its planner."""

    server: str
    name: str
    description: str
    schema: dict[str, Any] = field(default_factory=dict)

    @property
    def qualified(self) -> str:
        return f"{self.server}.{self.name}"


@dataclass(frozen=True)
class ToolCall:
    server: str
    name: str
    args: dict[str, Any]


@dataclass(frozen=True)
class ToolResult:
    call: ToolCall
    ok: bool
    data: Any = None
    error: str | None = None


# Translator decisions ---------------------------------------------------


@dataclass(frozen=True)
class Direct:
    """Translator handles the turn itself. No executor call."""

    text: str


@dataclass(frozen=True)
class Delegate:
    """Translator hands off to executor with a crisp instruction."""

    instruction: str


@dataclass(frozen=True)
class Clarify:
    """Translator needs more info from the user before acting."""

    question: str


TranslatorAction = Union[Direct, Delegate, Clarify]


# Executor exchange ------------------------------------------------------


@dataclass(frozen=True)
class ExecutorRequest:
    conversation: Conversation
    instruction: str


@dataclass(frozen=True)
class ExecutorResponse:
    summary: str  # natural-language sentence the translator can relay aloud
    tool_calls: list[ToolCall] = field(default_factory=list)
    tool_results: list[ToolResult] = field(default_factory=list)
    success: bool = True
    error: str | None = None
