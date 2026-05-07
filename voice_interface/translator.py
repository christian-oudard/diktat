"""Translator: the upper / lighter conversational agent.

In the real system this would be Haiku driving short turns with low
latency. Here it's a deterministic dialogue policy with three abilities:

    * Respond directly (chitchat, acknowledgements).
    * Ask the user a clarifying question (slot-filling, draft confirmation).
    * Delegate to the executor with a canonical natural-language instruction.

The translator owns the running ``Conversation`` and a small SMS state
machine for multi-turn interactions. Per the design sketch, it passes the
*full* conversation along when it delegates -- wasteful but dead simple.

SMS state machine -- three flows that all converge on ``Send SMS to <who>``:

    one-shot      "tell Lola I'll be home"            -> delegate immediately
    slot-fill     "send a text" / "Lola" / "..."      -> delegate when filled
    draft mode    "draft a text to Lola: ..."         -> confirm/revise loop
"""

from __future__ import annotations

import re
import time
from dataclasses import dataclass, field
from typing import Optional

from voice_interface.executor import Executor
from voice_interface.protocol import (
    Clarify,
    Conversation,
    Delegate,
    Direct,
    ExecutorRequest,
    TranslatorAction,
    Utterance,
)


# ---- pending-SMS state machine ------------------------------------------


@dataclass
class PendingSMS:
    """SMS we're collecting / drafting across turns."""

    recipient: Optional[str] = None
    message: Optional[str] = None
    needs_confirm: bool = False  # set when triggered by "draft ..."

    def missing(self) -> Optional[str]:
        if not self.recipient:
            return "recipient"
        if not self.message:
            return "message"
        return None


# ---- rule patterns -------------------------------------------------------


_GREETINGS = re.compile(r"^(hi|hey|hello|yo|howdy)\.?$", re.IGNORECASE)
_THANKS = re.compile(r"^(thanks|thank you|cheers|ty)\.?$", re.IGNORECASE)
_CANCEL = re.compile(r"^(cancel|nevermind|never mind|forget it|stop)\.?$", re.IGNORECASE)
_CONFIRM = re.compile(
    r"^(yes|yeah|yep|sure|send( it)?|go ahead|do it|confirm|ok|okay)\.?$",
    re.IGNORECASE,
)
_REVISE = re.compile(
    r"^(?:make it|change (?:it )?to|actually(?:,)? say|say instead|"
    r"actually,? make it)\s*[:\s]\s*(?P<msg>.+?)\.?$",
    re.IGNORECASE,
)

_LOG_PATS = (
    re.compile(r"^(?:log this|log|save|note)[:\s]+(?P<text>.+?)\.?$", re.IGNORECASE),
    re.compile(r"^(?:write|add) (?:this )?to (?:the )?log[:\s]+(?P<text>.+?)\.?$", re.IGNORECASE),
)

# Single-word ``who`` keeps the regex unambiguous. Multi-word contact names
# are out of scope for the mock; a real LLM-driven translator would resolve
# the recipient from the conversation rather than rely on regex shape.
_SMS_FULL_PATS = (
    re.compile(
        r"^(?:text|message)\s+(?P<who>\w[\w\-]*)\s*[:\s]\s*(?P<msg>.+?)\.?$",
        re.IGNORECASE,
    ),
    re.compile(
        r"^tell\s+(?P<who>\w[\w\-]*)\s*[:\s]\s*(?P<msg>.+?)\.?$",
        re.IGNORECASE,
    ),
    re.compile(
        r"^send\s+(?P<who>\w[\w\-]*)\s+a\s+(?:text|message|sms)\s+saying\s+(?P<msg>.+?)\.?$",
        re.IGNORECASE,
    ),
)

_SMS_BARE_PATS = (
    re.compile(r"^send a (?:text|sms|message)\.?$", re.IGNORECASE),
    re.compile(r"^text someone\.?$", re.IGNORECASE),
)

_DRAFT_FULL_PAT = re.compile(
    r"^draft\s+(?:a |the )?(?:text|message|sms)\s+to\s+(?P<who>\w[\w\-]*)\s*[:\s]\s*(?P<msg>.+?)\.?$",
    re.IGNORECASE,
)
_DRAFT_TO_PAT = re.compile(
    r"^draft\s+(?:a |the )?(?:text|message|sms)\s+to\s+(?P<who>\w[\w\-]*)\.?$",
    re.IGNORECASE,
)
_DRAFT_BARE_PAT = re.compile(
    r"^draft\s+(?:a |the )?(?:text|message|sms)\.?$",
    re.IGNORECASE,
)

_INBOX_PATS = (
    re.compile(r"^(?:any|got any|do i have any)\s+(?:new\s+)?messages\??\.?$", re.IGNORECASE),
    re.compile(r"^check (?:my\s+)?(?:inbox|messages|texts)\.?$", re.IGNORECASE),
    re.compile(r"^did (?:anyone|anybody) text me\??\.?$", re.IGNORECASE),
    re.compile(r"^read (?:my\s+)?(?:texts|inbox|messages)\.?$", re.IGNORECASE),
)

_SHOP_PAT = re.compile(
    r"^(?:buy|order)\s+(?:me\s+)?(?P<item>.+?)\s+(?:on|at|from)\s+(?P<vendor>[\w\.\-]+)\.?$",
    re.IGNORECASE,
)


def _clean_body(text: str) -> str:
    return text.strip().rstrip(".").strip()


# ---- translator ---------------------------------------------------------


@dataclass
class Translator:
    executor: Executor
    conversation: Conversation = field(default_factory=Conversation)
    pending_sms: PendingSMS | None = None
    # Vendor name -> URL. In the real system, executor (LLM) would resolve.
    vendor_urls: dict[str, str] = field(default_factory=dict)
    clock: callable = field(default=lambda: time.time())  # type: ignore[assignment]

    # ------- public entry points ----------------------------------------

    def handle_user(self, text: str, raw: str | None = None) -> str:
        """Process one user turn end-to-end. Returns the assistant utterance."""
        self.conversation.append(
            Utterance("user", text, raw_text=raw, timestamp=self.clock())
        )
        action = self.decide(text)
        response = self._execute_action(action)
        self.conversation.append(Utterance("assistant", response, timestamp=self.clock()))
        return response

    # ------- decision policy --------------------------------------------

    def decide(self, text: str) -> TranslatorAction:
        stripped = text.strip()

        # 1. Cancellation clears pending state.
        if _CANCEL.match(stripped):
            had_pending = self.pending_sms is not None
            self.pending_sms = None
            return Direct("Okay, cancelled." if had_pending else "Okay.")

        # 2. Pending-SMS turns dispatch into the state machine.
        if self.pending_sms is not None:
            return self._handle_pending_sms(stripped)

        # 3. Quick chit-chat shortcuts.
        if _GREETINGS.match(stripped):
            return Direct("Hi. What can I do?")
        if _THANKS.match(stripped):
            return Direct("You bet.")

        # 4. Log intent.
        for pat in _LOG_PATS:
            m = pat.match(stripped)
            if m:
                return Delegate(f"Append to log: {m.group('text').strip()}")

        # 5. Inbox intent.
        for pat in _INBOX_PATS:
            if pat.match(stripped):
                return Delegate("Read SMS inbox")

        # 6. Draft mode -- requires confirm before send.
        m = _DRAFT_FULL_PAT.match(stripped)
        if m:
            self.pending_sms = PendingSMS(
                recipient=m.group("who").strip(),
                message=_clean_body(m.group("msg")),
                needs_confirm=True,
            )
            return self._next_step()
        m = _DRAFT_TO_PAT.match(stripped)
        if m:
            self.pending_sms = PendingSMS(
                recipient=m.group("who").strip(), needs_confirm=True
            )
            return self._next_step()
        if _DRAFT_BARE_PAT.match(stripped):
            self.pending_sms = PendingSMS(needs_confirm=True)
            return self._next_step()

        # 7. Full SMS in one turn -- send immediately, no confirm.
        for pat in _SMS_FULL_PATS:
            m = pat.match(stripped)
            if m:
                who = m.group("who").strip()
                msg = _clean_body(m.group("msg"))
                return Delegate(f"Send SMS to {who} saying {msg}")

        # 8. Bare "send a text" -> start slot filling, no confirm.
        for pat in _SMS_BARE_PATS:
            if pat.match(stripped):
                self.pending_sms = PendingSMS()
                return self._next_step()

        # 9. Shop intent.
        m = _SHOP_PAT.match(stripped)
        if m:
            vendor = m.group("vendor").rstrip(".").lower()
            url = self.vendor_urls.get(vendor)
            if not url:
                return Direct(f"I don't know how to shop at {vendor} yet.")
            return Delegate(f"Shop for {m.group('item').strip()} at {url}")

        # 10. Last resort -- ask the user to rephrase rather than hallucinate.
        return Clarify("I’m not sure I caught that. Could you say it another way?")

    # ------- pending-SMS state machine ----------------------------------

    def _handle_pending_sms(self, text: str) -> TranslatorAction:
        p = self.pending_sms
        assert p is not None

        # Are we awaiting confirmation of a fully-drafted message?
        awaiting_confirm = (
            p.needs_confirm and p.recipient is not None and p.message is not None
        )
        if awaiting_confirm:
            if _CONFIRM.match(text):
                # Confirmed -> drop pending and delegate.
                who, msg = p.recipient, p.message
                self.pending_sms = None
                return Delegate(f"Send SMS to {who} saying {msg}")
            m = _REVISE.match(text)
            if m:
                p.message = _clean_body(m.group("msg"))
                return self._next_step()
            # Unrecognized -- treat the turn as a replacement message body.
            p.message = _clean_body(text)
            return self._next_step()

        # Otherwise we're slot-filling.
        missing = p.missing()
        if missing == "recipient":
            p.recipient = _clean_body(text)
        elif missing == "message":
            p.message = _clean_body(text)
        return self._next_step()

    def _next_step(self) -> TranslatorAction:
        """Inspect pending_sms and produce the appropriate next action."""
        p = self.pending_sms
        assert p is not None
        missing = p.missing()
        if missing == "recipient":
            return Clarify("Sure -- who should I text?")
        if missing == "message":
            return Clarify(f"What should I say to {p.recipient}?")
        if p.needs_confirm:
            return Clarify(f"Got it: “{p.message}”. Send?")
        # Filled and not requiring confirm -> delegate now.
        who, msg = p.recipient, p.message
        self.pending_sms = None
        return Delegate(f"Send SMS to {who} saying {msg}")

    # ------- delegation -------------------------------------------------

    def _execute_action(self, action: TranslatorAction) -> str:
        if isinstance(action, Direct):
            return action.text
        if isinstance(action, Clarify):
            return action.question
        if isinstance(action, Delegate):
            req = ExecutorRequest(self.conversation, action.instruction)
            resp = self.executor.execute(req)
            return resp.summary
        raise AssertionError(f"unknown action: {action!r}")
