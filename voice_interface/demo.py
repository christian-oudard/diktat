"""Scripted demo of the full voice interface stack.

Run with:
    uv run python -m voice_interface

Prints a transcript of a few scenarios end-to-end, so you can eyeball
what the agent does without setting up a real microphone or LLM. Each
scenario builds a fresh stack, scripts user turns, drains the
orchestrator, and prints what was said and what tools fired.
"""

from __future__ import annotations

import argparse
import sys
import textwrap
from pathlib import Path
from tempfile import TemporaryDirectory

from voice_interface.executor import Executor
from voice_interface.intents import RuleBasedPlanner
from voice_interface.mcp_servers import (
    BrowserServer,
    FakePage,
    LogServer,
    SMSServer,
)
from voice_interface.orchestrator import Orchestrator
from voice_interface.translator import Translator
from voice_interface.voice import VoiceLayer


# ---- stack wiring -------------------------------------------------------


def build_stack(log_path: Path) -> dict:
    log = LogServer(log_path)
    sms = SMSServer(contacts={"Lola": "+15555550100", "Bob": "+15555550199"})
    browser = BrowserServer(
        {
            "https://shop.example/buy": FakePage(
                url="https://shop.example/buy",
                title="Shop",
                body="things",
                forms={"buy": {"item": "string", "qty": "int"}},
            )
        }
    )
    executor = Executor([log, sms, browser], RuleBasedPlanner())
    translator = Translator(
        executor=executor,
        vendor_urls={"amazon": "https://shop.example/buy"},
    )
    voice = VoiceLayer()
    orch = Orchestrator(voice=voice, translator=translator)
    orch.watch_inbound_sms(sms)
    return dict(
        log=log, sms=sms, browser=browser, executor=executor,
        translator=translator, voice=voice, orch=orch,
    )


# ---- scenarios ----------------------------------------------------------


SCENARIOS: list[tuple[str, str, list]] = [
    (
        "black-triangle",
        "Voice command writes to a log file via MCP -- the smallest E2E loop.",
        [
            ("user", "uh, log this hello world"),
            ("user", "save: bought milk"),
        ],
    ),
    (
        "mvp-text",
        "Hold a text-message conversation with Lola entirely by voice.",
        [
            ("user", "uh, tell Lola I'll be like ten minutes late or so"),
        ],
    ),
    (
        "draft-revise",
        "Draft, revise, then send. Demonstrates the confirm-before-send loop.",
        [
            ("user", "draft a text to Lola: I'll be home soon"),
            ("user", "make it: I'll be home in 20 minutes"),
            ("user", "send it"),
        ],
    ),
    (
        "slot-fill",
        "User starts vague; translator fills slots over multiple turns.",
        [
            ("user", "send a text"),
            ("user", "Lola"),
            ("user", "see you tonight"),
        ],
    ),
    (
        "inbound-then-reply",
        "Phone surfaces an inbound SMS; user replies via voice.",
        [
            ("inbound_sms", ("Lola", "what's for dinner?")),
            ("user", "tell Lola: tacos"),
        ],
    ),
    (
        "shopping-v2",
        "V2 milestone: multi-step browser/shopping task in one voice command.",
        [
            ("user", "buy me milk on amazon"),
        ],
    ),
    (
        "edge-unknown-contact",
        "Unknown contact: agent surfaces the failure instead of silent send.",
        [
            ("user", "text Stranger: hello"),
        ],
    ),
    (
        "edge-cancellation",
        "User can back out of a pending action.",
        [
            ("user", "send a text"),
            ("user", "nevermind"),
        ],
    ),
]


# ---- transcript renderer -----------------------------------------------


def _format_call(call) -> str:
    args = ", ".join(f"{k}={v!r}" for k, v in call.args.items())
    return f"{call.server}.{call.name}({args})"


def run_scenario(name: str, blurb: str, script: list, out=sys.stdout) -> None:
    print(f"\n=== {name} ===", file=out)
    print(textwrap.fill(blurb, 78), file=out)
    print("-" * 78, file=out)

    with TemporaryDirectory() as tmp:
        stack = build_stack(Path(tmp) / "voice.log")
        orch: Orchestrator = stack["orch"]
        sms: SMSServer = stack["sms"]
        # Wrap each MCP call to print as it fires.
        original_execute = stack["executor"].execute

        def traced_execute(req):
            resp = original_execute(req)
            for call, result in zip(resp.tool_calls, resp.tool_results):
                ok = "ok" if result.ok else f"FAIL: {result.error}"
                print(f"    [tool] {_format_call(call)} -> {ok}", file=out)
            return resp

        stack["executor"].execute = traced_execute  # type: ignore[method-assign]

        # Run the script.
        for kind, payload in script:
            if kind == "user":
                print(f"  user: {payload}", file=out)
                stack["voice"].user_says(payload)
                while orch.step():
                    pass
            elif kind == "inbound_sms":
                from_, body = payload
                print(f"  [inbound sms] from {from_}: {body}", file=out)
                sms.deliver_inbound(from_, body)

        # Render the assistant transcript.
        for u in stack["translator"].conversation.utterances:
            if u.speaker == "assistant":
                print(f"  agent: {u.text}", file=out)

        # Final state surfaces.
        log_text = (Path(tmp) / "voice.log").read_text() if (Path(tmp) / "voice.log").exists() else ""
        if log_text:
            print(f"  [log] -> {log_text!r}", file=out)
        if sms.outbox:
            for m in sms.outbox:
                print(
                    f"  [sms outbox] to={m.to} ({m.contact}): {m.body!r}",
                    file=out,
                )
        if stack["browser"].submissions:
            for s in stack["browser"].submissions:
                print(f"  [browser submit] {s}", file=out)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Run scripted voice-interface scenarios."
    )
    parser.add_argument(
        "scenario", nargs="?", default="all",
        help=(
            "Name of scenario to run, or 'all' (default), or 'list'. "
            "Available: " + ", ".join(name for name, _, _ in SCENARIOS)
        ),
    )
    args = parser.parse_args(argv)

    if args.scenario == "list":
        for name, blurb, _ in SCENARIOS:
            print(f"  {name:24s} {blurb}")
        return 0

    if args.scenario == "all":
        for name, blurb, script in SCENARIOS:
            run_scenario(name, blurb, script)
        return 0

    for name, blurb, script in SCENARIOS:
        if name == args.scenario:
            run_scenario(name, blurb, script)
            return 0
    print(f"unknown scenario: {args.scenario}", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
