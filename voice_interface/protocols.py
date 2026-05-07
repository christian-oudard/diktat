"""Translator <-> Executor interface variants.

The design sketch leaves this open: "Simplest version for proof of
concept: translator passes the full conversation history down to the
executor." That's variant A below. We want to compare it against other
plausible protocols and see where each falls down.

Each variant is a *prompt strategy*. The harness builds the prompts, an
LLM (real or sub-agent) produces JSON, the harness dispatches against
the in-process tool surfaces, and we observe side effects.

Variants compared
-----------------

    A  full-context           translator passes raw transcript + instruction;
                              executor sees everything and decides.
    B  structured-handoff     translator parses to JSON intent objects;
                              executor is a thin dispatcher.
    C  domain-namespaced      translator picks a domain (sms, log, ...);
                              executor sees only that domain's tools.
    D  plan-by-translator     translator emits the literal tool calls;
                              there is no executor stage at all.
    E  single-agent           one agent with all tools. Reference
                              architecture; tests whether two layers earn
                              their cost.

Stress scenario
---------------

User says one sentence that requires anaphora resolution and two tool
calls (SMS + log) at once. History contains an inbound message from
Lola, so 'her' must resolve to Lola.

Findings (one run -- see scripts/run_protocol_experiment.py)
------------------------------------------------------------

All five variants produced correct side effects (SMS to Lola containing
'7' and 'place', plus a log line about the dinner). Where they differed:

    variant                pass  tokens  latency_ms  stages
    A  full-context         ok    11.5k       4.1k    1
    B  structured-handoff   ok    22.8k       5.5k    2 sequential
    C  domain-namespaced    ok    34.0k       5.4k    1 + 2 parallel
    D  plan-by-translator   ok    11.4k       2.2k    1
    E  single-agent         ok    11.4k       1.9k    1

Takeaways
---------

1. **Two-layer always pays a 2-3x token premium.** The same intent
   gets re-expressed; the executor is essentially saying back what the
   translator just said. Worth it only if the translator runs on a
   meaningfully cheaper model than the executor, or if you need the
   property below.

2. **B is dominated by D.** Same parse, but D collapses the two stages
   into one. The "translator-as-planner" pattern is the structural form
   of B without the duplicate round-trip.

3. **C earns its cost only when blast radius matters.** Each domain
   executor sees only its own tool, so a confused or compromised
   executor cannot fire SMS from the log lane or vice versa. That's
   real value for sensitive tools (payments, deletions, credentials)
   but unnecessary for chat + log.

4. **E (single-agent) is the cheapest reference.** The two-layer split
   needs an explicit reason -- cost arbitrage, blast radius, or
   perceived latency from streaming a translator chitchat ("ok, one
   sec...") while the executor works. The latter is real but isn't
   modeled by these one-shot prompts.

5. **The design sketch's variant A is competitive with E** when the
   translator and executor are the *same* model. The split only buys
   anything when the translator is a smaller/cheaper tier.

Recommendation
--------------

* Start with **E** for the prototype. Add a translator layer only when
  (a) you can swap in a cheaper model for the translator, *or*
  (b) you need C-style domain isolation for sensitive tools.
* If you do split, prefer **A** (full-context pass-through) over B for
  general use; **C** for sensitive lanes only.
* **D** is a useful waypoint -- it shows how thin the executor needs
  to be when the translator already plans.

Open questions this experiment doesn't answer
---------------------------------------------

* How does each variant behave on **failure recovery** (a tool errors
  mid-plan)? E and A can decide adaptively; D pre-commits.
* Does the translator's chitchat stream (perceived latency benefit)
  outweigh the token cost? Needs a streaming setup to test.
* What happens with a **smaller-tier translator** (Haiku-class)? The
  general-purpose sub-agent here is too capable to expose its limits.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


# ---- the stress scenario -----------------------------------------------


STRESS = {
    "name": "dinner-and-log",
    "contacts": {"Lola": "+15555550100", "Bob": "+15555550199"},
    "history": [
        {"role": "assistant",
         "content": "New message from Lola: dinner tonight?"},
    ],
    "user": "yeah, just tell her 7 at our place and log it",
    # We'll judge a run a success if BOTH:
    #   1. SMS to Lola whose body mentions '7' and 'place'
    #   2. log line that mentions 'dinner' or 'Lola' or '7' or 'place'
}


# ---- shared prompt fragments -------------------------------------------


_HEADER = (
    "Output JSON only. No prose, no markdown fences. "
    "Single line, single object, parseable.\n"
)

_TOOL_DOCS = {
    "sms_send": (
        "sms_send(recipient, message): send SMS to a known contact. "
        "recipient must be a real contact name (not a pronoun)."
    ),
    "log_write": (
        "log_write(text): append a single line to the user's voice log."
    ),
}

_CONTACTS = "CONTACTS:\n  Lola: +15555550100\n  Bob:  +15555550199\n"

_HISTORY = (
    "CONVERSATION SO FAR:\n"
    "  assistant: \"New message from Lola: dinner tonight?\"\n"
)

_USER = "USER: 'yeah, just tell her 7 at our place and log it'\n"


# ---- Variant A: full-context pass-through (the design-sketch baseline) -


def prompt_a() -> str:
    return (
        "You are the EXECUTOR. The translator has handed you the full "
        "conversation and a free-form instruction. Decide what tool calls "
        "to make.\n\n"
        + _HEADER
        + 'Output: {"calls": [ {"tool": "...", "args": {...}}, ... ]} '
        '  -- empty list means no action; use {"say": "..."} only if no '
        'tool call is appropriate.\n\n'
        + _CONTACTS + "\n"
        + _HISTORY + "\n"
        + _USER + "\n"
        + "TOOLS:\n"
        + f"  {_TOOL_DOCS['sms_send']}\n"
        + f"  {_TOOL_DOCS['log_write']}\n\n"
        + "INSTRUCTION FROM TRANSLATOR: 'do whatever the user just asked'.\n"
    )


# ---- Variant B: structured handoff -------------------------------------


def prompt_b_translator() -> str:
    """Translator: parse into intent objects. Executor never sees the raw
    transcript."""
    return (
        "You are the TRANSLATOR (a fast model). Parse the user's turn into "
        "zero or more intent objects. Resolve pronouns to contact names "
        "from CONTACTS. Do not call any tools yourself.\n\n"
        + _HEADER
        + 'Output: {"intents": ['
        '  {"kind": "sms.send", "recipient": "...", "message": "..."},\n'
        '  {"kind": "log.write", "text": "..."},\n'
        '  ...\n'
        ']}\n'
        '  -- or {"say": "..."} if no action is appropriate.\n\n'
        + _CONTACTS + "\n"
        + _HISTORY + "\n"
        + _USER
    )


def prompt_b_executor(intents_json: str) -> str:
    """Executor: dispatch intents to actual tools. Sees no transcript."""
    return (
        "You are the EXECUTOR (a heavy model). The translator has parsed "
        "the user's turn into intent objects. Map each intent to the right "
        "tool call.\n\n"
        + _HEADER
        + 'Output: {"calls": [ {"tool": "...", "args": {...}}, ... ]}\n\n'
        + "TOOLS:\n"
        + f"  {_TOOL_DOCS['sms_send']}\n"
        + f"  {_TOOL_DOCS['log_write']}\n\n"
        + "INTENTS:\n"
        + f"  {intents_json}\n"
    )


# ---- Variant C: domain-namespaced --------------------------------------


def prompt_c_translator() -> str:
    """Translator: pick the domains that need to act. Each domain gets a
    free-form instruction; the executor in that domain only sees that
    domain's tools."""
    return (
        "You are the TRANSLATOR. Decide which domains need to act and what "
        "each should do (free-form instructions per domain). Resolve "
        "pronouns from CONTACTS.\n\n"
        + _HEADER
        + 'Output: {"calls_to": ['
        '  {"domain": "sms", "instruction": "..."},\n'
        '  {"domain": "log", "instruction": "..."}\n'
        ']}\n\n'
        + _CONTACTS + "\n"
        + _HISTORY + "\n"
        + _USER + "\n"
        + "AVAILABLE DOMAINS: sms, log.\n"
    )


def prompt_c_executor(domain: str, instruction: str) -> str:
    tool = _TOOL_DOCS["sms_send"] if domain == "sms" else _TOOL_DOCS["log_write"]
    return (
        f"You are the EXECUTOR for the '{domain}' domain. The translator "
        f"asked you: {instruction!r}. You only have one tool. Call it.\n\n"
        + _HEADER
        + 'Output: {"calls": [ {"tool": "...", "args": {...}} ]}\n\n'
        + f"CONTACTS:\n  Lola: +15555550100\n  Bob:  +15555550199\n\n"
        + f"TOOL:\n  {tool}\n"
    )


# ---- Variant D: translator-as-planner (no executor stage) --------------


def prompt_d() -> str:
    return (
        "You are the TRANSLATOR acting as planner. Emit the literal tool "
        "calls directly -- there is no executor stage. Resolve pronouns.\n\n"
        + _HEADER
        + 'Output: {"calls": [ {"tool": "...", "args": {...}}, ... ]}\n\n'
        + _CONTACTS + "\n"
        + _HISTORY + "\n"
        + _USER + "\n"
        + "TOOLS:\n"
        + f"  {_TOOL_DOCS['sms_send']}\n"
        + f"  {_TOOL_DOCS['log_write']}\n"
    )


# ---- Variant E: single-agent (no translator at all) --------------------


def prompt_e() -> str:
    return (
        "You are the ONLY AGENT. You have voice I/O and tool access. "
        "Decide what to do.\n\n"
        + _HEADER
        + 'Output: {"calls": [ {"tool": "...", "args": {...}}, ... ]}\n\n'
        + _CONTACTS + "\n"
        + _HISTORY + "\n"
        + _USER + "\n"
        + "TOOLS:\n"
        + f"  {_TOOL_DOCS['sms_send']}\n"
        + f"  {_TOOL_DOCS['log_write']}\n"
    )


# ---- shared dispatcher / scoring ---------------------------------------


@dataclass
class Outcome:
    variant: str
    stages: list[dict[str, Any]] = field(default_factory=list)
    sms_outbox: list[Any] = field(default_factory=list)
    log_text: str = ""
    score: dict[str, bool] = field(default_factory=dict)
    notes: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return bool(self.score) and all(self.score.values())


def evaluate(outcome: Outcome) -> Outcome:
    """Set ``outcome.score`` based on side effects. Mutates and returns."""
    has_sms = any(
        m.contact == "Lola" and "7" in m.body and ("place" in m.body.lower())
        for m in outcome.sms_outbox
    )
    log = outcome.log_text.lower()
    log_mentions = ("dinner" in log) or ("lola" in log) or ("7" in log) or ("place" in log)
    outcome.score = {
        "sms_to_lola_with_7_and_place": has_sms,
        "log_mentions_event": log_mentions,
        "exactly_one_sms": len([m for m in outcome.sms_outbox if m.contact == "Lola"]) == 1,
    }
    return outcome
