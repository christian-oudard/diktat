"""Run a scenario against an LLM-driven voice agent.

The agent's reply is expected to be a single line of JSON:

    {"tool": "<name>", "args": {...}}             -- to call a tool
    {"say": "..."}                                -- to talk back, no tool

The harness:

    1. Builds a prompt from the scenario (state of world + tool catalog).
    2. Runs the LLM (any callable matching the protocol).
    3. Parses the JSON, dispatches the tool call against in-process state.
    4. Runs the scenario's ``check`` against the resulting world.

In production the LLM is the Anthropic SDK. In tests, you can plug in a
canned LLM that returns scripted JSON. To exercise the *intelligence* you
spawn a sub-agent: see ``voice_interface/sub_agent_runner.py``.
"""

from __future__ import annotations

import json
import textwrap
from dataclasses import dataclass, field
from typing import Any, Callable

from voice_interface.mcp_servers import LogServer, SMSServer
from voice_interface.scenarios import Scenario


# ---- the tool catalog the agent sees -----------------------------------


TOOL_CATALOG = {
    "log_write": {
        "description": "Append a single line to the user's voice log.",
        "schema": {"text": "string"},
    },
    "sms_send": {
        "description": (
            "Send an SMS to a known contact. Recipient must be a contact "
            "name (e.g. 'Lola'), not a pronoun. Refuse to call this tool "
            "if the recipient is not in the contacts table -- talk back "
            "instead, asking the user to add the contact."
        ),
        "schema": {"recipient": "string", "message": "string"},
    },
}


# ---- the world we wire the agent's tool calls into ----------------------


@dataclass
class World:
    log: LogServer
    sms: SMSServer

    def snapshot(self) -> dict[str, Any]:
        log_text = self.log.path.read_text() if self.log.path.exists() else ""
        return {"log": log_text, "sms": list(self.sms.outbox)}


def build_world(scenario: Scenario, log_path) -> World:
    log = LogServer(log_path)
    sms = SMSServer(contacts=scenario.contacts)
    for who, body in scenario.inbound:
        sms.deliver_inbound(who, body)
    return World(log=log, sms=sms)


# ---- prompt construction ------------------------------------------------


def build_prompt(scenario: Scenario) -> str:
    """The exact text given to the agent. Self-contained -- no system messages
    needed; all context goes in the prompt body."""
    parts = [
        "You are a voice agent. The user is talking to you over a headset.",
        "Decide what to do in response to the user's latest turn.",
        "",
        "OUTPUT FORMAT -- output exactly one line of JSON, no prose, no fences:",
        '  {"tool": "<name>", "args": {...}}     to call a tool',
        '  {"say": "<words to speak>"}           to talk back without acting',
        "",
    ]
    if scenario.contacts:
        parts.append("CONTACTS:")
        for k, v in scenario.contacts.items():
            parts.append(f"  {k}: {v}")
        parts.append("")
    if scenario.history:
        parts.append("CONVERSATION SO FAR:")
        for msg in scenario.history:
            parts.append(f"  {msg['role']}: {msg['content']}")
        parts.append("")
    parts.append("TOOLS AVAILABLE:")
    for name in scenario.tools:
        spec = TOOL_CATALOG[name]
        parts.append(f"  {name}({', '.join(spec['schema'])})")
        parts.append(f"    {spec['description']}")
    parts.append("")
    parts.append(f"USER: {scenario.user!r}")
    return "\n".join(parts)


# ---- dispatching the agent's reply --------------------------------------


def parse_reply(reply: str) -> dict[str, Any]:
    """Parse the agent's reply into either a tool call or a 'say' object.

    Tolerant: strips code fences and stray prose around the JSON.
    """
    text = reply.strip()
    # Strip markdown fences if present.
    if text.startswith("```"):
        text = text.strip("`")
        if text.startswith("json"):
            text = text[4:]
        text = text.strip()
    # Find the first {...} JSON object on a single line.
    start = text.find("{")
    end = text.rfind("}")
    if start == -1 or end == -1:
        raise ValueError(f"no JSON object in reply: {reply!r}")
    return json.loads(text[start : end + 1])


def dispatch(world: World, parsed: dict[str, Any]) -> str:
    """Apply the parsed agent reply to the world. Returns a description."""
    if "say" in parsed:
        return f"[say] {parsed['say']!r}"
    name = parsed["tool"]
    args = parsed.get("args", {})
    if name == "log_write":
        result = world.log.call_tool("write", args)
    elif name == "sms_send":
        result = world.sms.call_tool(
            "send",
            {"recipient": args.get("recipient"), "message": args.get("message")},
        )
    else:
        return f"[unknown tool] {name}"
    return f"[tool] {name}({args}) -> {'ok' if result.ok else 'FAIL: ' + (result.error or '')}"


# ---- the run() entry point ----------------------------------------------


def run(
    scenario: Scenario,
    llm: Callable[[str], str],
    log_path,
) -> dict[str, Any]:
    """Run one scenario. ``llm`` is any callable from prompt -> reply text."""
    world = build_world(scenario, log_path)
    prompt = build_prompt(scenario)
    reply = llm(prompt)
    parsed = parse_reply(reply)
    action_log = dispatch(world, parsed)
    snap = world.snapshot()
    err = scenario.check(snap)
    return {
        "scenario": scenario.name,
        "prompt": prompt,
        "reply": reply,
        "parsed": parsed,
        "action": action_log,
        "world": snap,
        "ok": err is None,
        "error": err,
    }
