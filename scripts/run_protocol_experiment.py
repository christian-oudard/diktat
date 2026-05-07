"""Score the translator/executor protocol variants.

Runs the captured sub-agent JSON replies (from a real run) through the
in-process tool surfaces (LogServer + SMSServer) and reports:

    * which calls fired
    * the resulting outbox / log file
    * pass/fail against the stress scenario's success criteria
    * total tokens used (approximate -- recorded from the agent runs)
    * total wall-clock latency (max of stages, parallel where applicable)

Useful for eyeballing tradeoffs side by side. Replace the captured
replies dict with fresh ones to re-score after another agent run.
"""

from __future__ import annotations

import json
import tempfile
from pathlib import Path

from voice_interface.mcp_servers import LogServer, SMSServer
from voice_interface.protocols import STRESS, evaluate, Outcome


# ---- captured replies from the latest sub-agent run --------------------

REPLIES = {
    "A_full_context": {
        "stages": [
            {"tokens": 11465, "duration_ms": 4145, "reply":
             '{"calls":[{"tool":"sms_send","args":{"recipient":"Lola","message":"7 at our place"}},'
             '{"tool":"log_write","args":{"text":"Told Lola 7 at our place for dinner tonight."}}]}'},
        ],
        "parallel_stages": False,
    },
    "B_structured_handoff": {
        "stages": [
            {"tokens": 11398, "duration_ms": 3109, "reply":
             '{"intents":[{"kind":"sms.send","recipient":"Lola","message":"7 at our place"},'
             '{"kind":"log.write","text":"Told Lola 7 at our place for dinner tonight"}]}'},
            {"tokens": 11393, "duration_ms": 2349, "reply":
             '{"calls":[{"tool":"sms_send","args":{"recipient":"Lola","message":"7 at our place"}},'
             '{"tool":"log_write","args":{"text":"Told Lola 7 at our place for dinner tonight"}}]}'},
        ],
        "parallel_stages": False,
    },
    "C_domain_namespaced": {
        "stages": [
            {"tokens": 11392, "duration_ms": 2621, "reply":
             '{"calls_to":[{"domain":"sms","instruction":"Send SMS to Lola (+15555550100): \\"7 at our place\\""},'
             '{"domain":"log","instruction":"Log that user confirmed dinner with Lola tonight at 7 at our place."}]}'},
            {"tokens": 11300, "duration_ms": 1783, "reply":
             '{"calls":[{"tool":"sms_send","args":{"recipient":"Lola","message":"7 at our place"}}]}',
             "parallel_with": "stage_2_log"},
            {"tokens": 11296, "duration_ms": 2771, "reply":
             '{"calls":[{"tool":"log_write","args":{"text":"User confirmed dinner with Lola (+15555550100) tonight at 7pm at our place."}}]}',
             "parallel_with": "stage_2_sms"},
        ],
        "parallel_stages": True,
    },
    "D_planner_only": {
        "stages": [
            {"tokens": 11406, "duration_ms": 2166, "reply":
             '{"calls":[{"tool":"sms_send","args":{"recipient":"Lola","message":"7 at our place"}},'
             '{"tool":"log_write","args":{"text":"Confirmed dinner with Lola at 7 at our place."}}]}'},
        ],
        "parallel_stages": False,
    },
    "E_single_agent": {
        "stages": [
            {"tokens": 11385, "duration_ms": 1950, "reply":
             '{"calls":[{"tool":"sms_send","args":{"recipient":"Lola","message":"7 at our place"}},'
             '{"tool":"log_write","args":{"text":"Dinner with Lola at 7 at our place"}}]}'},
        ],
        "parallel_stages": False,
    },
}


# ---- dispatch ----------------------------------------------------------


def dispatch_calls(calls: list[dict], log: LogServer, sms: SMSServer) -> list[str]:
    log_lines: list[str] = []
    for c in calls:
        tool = c["tool"]
        args = c.get("args", {})
        if tool == "sms_send":
            r = sms.call_tool("send", args)
        elif tool == "log_write":
            r = log.call_tool("write", {"text": args.get("text", "")})
        else:
            r = None
        log_lines.append(
            f"  -> {tool}({args}) {'ok' if r and r.ok else 'FAIL: ' + (r.error if r else 'unknown tool')}"
        )
    return log_lines


def run_variant(name: str, payload: dict, tmp: Path) -> Outcome:
    log = LogServer(tmp / f"{name}.log")
    sms = SMSServer(contacts=STRESS["contacts"])
    outcome = Outcome(variant=name)

    # Find which stages produce executable calls. For parallel variants
    # all stages-after-the-first are leaves; for sequential ones only the
    # last stage is.
    if payload.get("parallel_stages"):
        leaf_stages = payload["stages"][1:]
    else:
        leaf_stages = [payload["stages"][-1]]

    all_calls: list[dict] = []
    for stage in leaf_stages:
        parsed = json.loads(stage["reply"])
        calls = parsed.get("calls", [])
        if not calls and "say" in parsed:
            outcome.notes.append(f"agent talked back: {parsed['say']!r}")
        all_calls.extend(calls)

    outcome.stages = list(payload["stages"])
    log_lines = dispatch_calls(all_calls, log, sms)

    outcome.sms_outbox = list(sms.outbox)
    outcome.log_text = log.path.read_text() if log.path.exists() else ""
    outcome.notes.extend(log_lines)
    return evaluate(outcome)


def total_tokens(payload: dict) -> int:
    return sum(s["tokens"] for s in payload["stages"])


def total_latency_ms(payload: dict) -> int:
    """Sum sequential stages; parallel stages count as max."""
    if not payload.get("parallel_stages"):
        return sum(s["duration_ms"] for s in payload["stages"])
    # First stage is sequential; remaining stages are parallel.
    head = payload["stages"][0]
    rest = payload["stages"][1:]
    return head["duration_ms"] + max(s["duration_ms"] for s in rest)


def main():
    with tempfile.TemporaryDirectory() as t:
        tmp = Path(t)
        rows = []
        for name, payload in REPLIES.items():
            outcome = run_variant(name, payload, tmp)
            rows.append((name, outcome, payload))

        # Print comparison table.
        print(f"\n{'variant':28s} {'pass':5s} {'tokens':>7s} {'latency_ms':>10s}  notes")
        print("-" * 92)
        for name, outcome, payload in rows:
            verdict = "PASS" if outcome.passed else "FAIL"
            tok = total_tokens(payload)
            lat = total_latency_ms(payload)
            print(f"{name:28s} {verdict:5s} {tok:>7d} {lat:>10d}")
            for k, v in outcome.score.items():
                mark = " ok" if v else "miss"
                print(f"  [{mark}] {k}")
            for note in outcome.notes:
                print(f"  {note}")
            print(f"  log: {outcome.log_text!r}")
            for m in outcome.sms_outbox:
                print(f"  sms: to={m.to} body={m.body!r}")
            print()


if __name__ == "__main__":
    main()
