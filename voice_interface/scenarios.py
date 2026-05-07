"""Voice-agent scenarios as plain data.

Each scenario describes:

    * The state of the world before the turn (contacts, inbound messages,
      conversation so far).
    * The user utterance being tested.
    * The tools the agent has access to.
    * What side effects must be true after the agent acts (assertions over
      the in-process tool surfaces).

These scenarios are the *brief* given to a sub-agent (or a real LLM call)
playing the voice-agent role. The harness in :mod:`voice_interface.harness`
turns scenarios into prompts, dispatches tool calls, and checks effects.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable


@dataclass
class Scenario:
    name: str
    blurb: str
    contacts: dict[str, str] = field(default_factory=dict)
    inbound: list[tuple[str, str]] = field(default_factory=list)  # (from, body)
    history: list[dict[str, str]] = field(default_factory=list)  # conv so far
    user: str = ""
    tools: list[str] = field(default_factory=list)  # tool names from catalog
    # ``check`` returns None on success or an error string on failure. It is
    # given the in-process world (log path text, sms outbox, etc.).
    check: Callable[[dict[str, Any]], str | None] = lambda world: None


def _outbox_has(to: str, body_substr: str) -> Callable:
    def check(world):
        for msg in world["sms"]:
            if msg.to == to and body_substr in msg.body:
                return None
        return f"no SMS to {to} containing {body_substr!r}; outbox={world['sms']}"
    return check


def _log_contains(substr: str) -> Callable:
    def check(world):
        if substr in world["log"]:
            return None
        return f"log missing {substr!r}; got {world['log']!r}"
    return check


def _outbox_empty() -> Callable:
    def check(world):
        if not world["sms"]:
            return None
        return f"expected empty outbox; got {world['sms']}"
    return check


SCENARIOS: list[Scenario] = [
    Scenario(
        name="black-triangle",
        blurb="Voice command writes to a log file. Smallest end-to-end loop.",
        user="uh, log this hello world",
        tools=["log_write"],
        check=_log_contains("hello world"),
    ),
    Scenario(
        name="mvp-text-lola",
        blurb="One-shot text to a known contact. Disfluencies stripped.",
        contacts={"Lola": "+15555550100"},
        user="uh, tell Lola I'll be like ten minutes late or so",
        tools=["sms_send"],
        check=_outbox_has("+15555550100", "ten minutes late"),
    ),
    Scenario(
        name="anaphora-her-after-inbound",
        blurb="Pronoun resolves to the most recent inbound contact.",
        contacts={"Lola": "+15555550100", "Bob": "+15555550199"},
        history=[
            {"role": "assistant",
             "content": "New message from Lola: what's for dinner?"},
        ],
        user="tell her tacos",
        tools=["sms_send"],
        check=_outbox_has("+15555550100", "tacos"),
    ),
    Scenario(
        name="unknown-contact",
        blurb="Don't silently send to an unknown name; surface the issue.",
        contacts={"Lola": "+15555550100"},
        user="text Stranger: hello",
        tools=["sms_send"],
        check=_outbox_empty(),
    ),
    Scenario(
        name="chitchat-no-tool",
        blurb="Greeting needs no tool call. Agent should just talk back.",
        user="hi",
        tools=["log_write", "sms_send"],
        check=lambda world: (
            None if not world["sms"] and not world["log"]
            else f"unexpected side effects: sms={world['sms']}, log={world['log']!r}"
        ),
    ),
]
