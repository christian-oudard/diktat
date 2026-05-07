"""Rule-based mock for the executor's planner.

Each ``Intent`` knows how to recognize a canonical instruction phrase, plan
the tool calls it implies, and summarize the result for the translator. The
``RuleBasedPlanner`` walks intents in order and uses the first match.

This is the *mock* analog of "ask Claude with conversation + tool catalog,
parse tool_use blocks". When the real planner ships, it replaces this whole
file; the rest of the system doesn't change.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Callable

from voice_interface.executor import Plan, Planner
from voice_interface.protocol import (
    ExecutorRequest,
    ToolCall,
    ToolResult,
    ToolSpec,
)


@dataclass
class Intent:
    name: str
    pattern: re.Pattern[str]
    plan_fn: Callable[[re.Match[str]], list[ToolCall]]
    summarize_fn: Callable[[re.Match[str], list[ToolResult]], str]


def _strip_quotes(s: str) -> str:
    s = s.strip()
    if len(s) >= 2 and s[0] == s[-1] and s[0] in {'"', "'"}:
        return s[1:-1]
    return s.strip(" .")


# ---- log -----------------------------------------------------------------

_log_intent = Intent(
    name="log.write",
    pattern=re.compile(r"^append to log:\s*(?P<text>.+)$", re.IGNORECASE),
    plan_fn=lambda m: [ToolCall("log", "write", {"text": _strip_quotes(m.group("text"))})],
    summarize_fn=lambda m, results: (
        f"Logged “{_strip_quotes(m.group('text'))}”."
        if results and results[0].ok
        else f"Couldn’t log it: {results[0].error if results else 'no result'}."
    ),
)


# ---- sms.send -----------------------------------------------------------

_sms_send_pat = re.compile(
    r"^send sms to (?P<who>[\w\s\-]+?) saying (?P<msg>.+)$",
    re.IGNORECASE,
)


def _plan_sms_send(m: re.Match[str]) -> list[ToolCall]:
    return [
        ToolCall(
            "sms",
            "send",
            {"recipient": m.group("who").strip(), "message": _strip_quotes(m.group("msg"))},
        )
    ]


def _summarize_sms_send(m: re.Match[str], results: list[ToolResult]) -> str:
    who = m.group("who").strip()
    msg = _strip_quotes(m.group("msg"))
    if not results:
        return "Didn’t send anything."
    r = results[0]
    if r.ok:
        return f"Texted {who}: “{msg}”."
    if r.error and "unknown contact" in r.error:
        return f"I don’t have a number for {who}. Want to add one?"
    return f"Couldn’t text {who}: {r.error}."


_sms_send_intent = Intent(
    name="sms.send",
    pattern=_sms_send_pat,
    plan_fn=_plan_sms_send,
    summarize_fn=_summarize_sms_send,
)


# ---- sms.inbox ----------------------------------------------------------


def _summarize_inbox(m: re.Match[str], results: list[ToolResult]) -> str:
    if not results or not results[0].ok:
        return "Couldn’t check the inbox."
    msgs = results[0].data or []
    if not msgs:
        return "No new messages."
    if len(msgs) == 1:
        msg = msgs[0]
        return f"One new message from {msg['from']}: “{msg['body']}”."
    head = ", ".join(m["from"] for m in msgs[:-1])
    last = msgs[-1]["from"]
    return f"You have {len(msgs)} new messages, from {head}, and {last}."


_sms_inbox_intent = Intent(
    name="sms.inbox",
    pattern=re.compile(r"^read sms inbox\.?$", re.IGNORECASE),
    plan_fn=lambda m: [ToolCall("sms", "inbox", {"unread_only": True})],
    summarize_fn=_summarize_inbox,
)


# ---- browser.shop -------------------------------------------------------

_shop_pat = re.compile(r"^shop for (?P<query>.+?) at (?P<url>https?://\S+)$", re.IGNORECASE)


def _plan_shop(m: re.Match[str]) -> list[ToolCall]:
    url = m.group("url")
    query = _strip_quotes(m.group("query"))
    return [
        ToolCall("browser", "navigate", {"url": url}),
        ToolCall("browser", "read", {}),
        ToolCall("browser", "submit", {"form": "buy", "fields": {"item": query, "qty": 1}}),
    ]


def _summarize_shop(m: re.Match[str], results: list[ToolResult]) -> str:
    query = _strip_quotes(m.group("query"))
    if all(r.ok for r in results):
        return f"Ordered {query}."
    failing = next((r for r in results if not r.ok), None)
    return f"Couldn’t finish ordering {query}: {failing.error if failing else 'unknown error'}."


_shop_intent = Intent(
    name="browser.shop",
    pattern=_shop_pat,
    plan_fn=_plan_shop,
    summarize_fn=_summarize_shop,
)


# ---- planner -------------------------------------------------------------


DEFAULT_INTENTS: tuple[Intent, ...] = (
    _log_intent,
    _sms_send_intent,
    _sms_inbox_intent,
    _shop_intent,
)


class RuleBasedPlanner(Planner):
    def __init__(self, intents: tuple[Intent, ...] = DEFAULT_INTENTS):
        self.intents = intents

    def _match(self, instruction: str) -> tuple[Intent, re.Match[str]] | None:
        text = instruction.strip()
        for intent in self.intents:
            m = intent.pattern.match(text)
            if m:
                return intent, m
        return None

    def plan(self, request: ExecutorRequest, tools: list[ToolSpec]) -> Plan:
        match = self._match(request.instruction)
        if not match:
            return Plan(tool_calls=[], notes={"unmatched": True})
        intent, m = match
        return Plan(tool_calls=intent.plan_fn(m), notes={"intent": intent.name, "match": m})

    def summarize(
        self,
        request: ExecutorRequest,
        plan: Plan,
        results: list[ToolResult],
    ) -> str:
        notes = plan.notes or {}
        if notes.get("unmatched"):
            return f"I’m not sure how to do that: {request.instruction!r}."
        intent: Intent = next(i for i in self.intents if i.name == notes.get("intent"))
        m: re.Match[str] = notes["match"]  # type: ignore[assignment]
        return intent.summarize_fn(m, results)
