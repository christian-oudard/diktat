"""Translator tests.

Pin down the dialogue policy: when does the translator handle a turn
itself, when does it ask for clarification, and when does it delegate.
The instructions it emits when delegating must match the executor's
intent grammar (see test_executor.py).
"""

import pytest

from voice_interface.executor import Executor
from voice_interface.intents import RuleBasedPlanner
from voice_interface.mcp_servers import LogServer, SMSServer
from voice_interface.protocol import Clarify, Delegate, Direct
from voice_interface.translator import Translator


@pytest.fixture
def translator(tmp_path):
    log = LogServer(tmp_path / "voice.log")
    sms = SMSServer(contacts={"Lola": "+15555550100", "Bob": "+15555550199"})
    ex = Executor([log, sms], RuleBasedPlanner())
    t = Translator(executor=ex, vendor_urls={"amazon": "https://shop.example/buy"})
    return t


class TestTranslatorDecisions:
    """``decide`` is the pure dialogue policy -- no executor side effects."""

    def test_greeting_handled_directly(self, translator):
        assert isinstance(translator.decide("Hi."), Direct)

    def test_log_intent_delegates(self, translator):
        a = translator.decide("Log this: hello world.")
        assert isinstance(a, Delegate) and a.instruction == "Append to log: hello world"

    def test_log_alt_phrasing(self, translator):
        a = translator.decide("Save: dinner with Lola Saturday")
        assert isinstance(a, Delegate)
        assert a.instruction == "Append to log: dinner with Lola Saturday"

    def test_full_text_to_contact(self, translator):
        a = translator.decide("Text Lola: I'll be home soon.")
        assert isinstance(a, Delegate)
        assert a.instruction == "Send SMS to Lola saying I'll be home soon"

    def test_tell_phrasing(self, translator):
        a = translator.decide("Tell Bob I'll bring drinks.")
        assert isinstance(a, Delegate)
        assert a.instruction == "Send SMS to Bob saying I'll bring drinks"

    def test_send_X_a_text_phrasing(self, translator):
        a = translator.decide("Send Lola a text saying running late.")
        assert isinstance(a, Delegate)
        assert a.instruction == "Send SMS to Lola saying running late"

    def test_inbox_check(self, translator):
        a = translator.decide("Did anyone text me?")
        assert isinstance(a, Delegate) and a.instruction == "Read SMS inbox"

    def test_inbox_alt_phrasings(self, translator):
        for phrase in ["Any new messages?", "Check my inbox.", "Read my texts."]:
            a = translator.decide(phrase)
            assert isinstance(a, Delegate), phrase
            assert a.instruction == "Read SMS inbox"

    def test_bare_send_text_starts_clarification(self, translator):
        a = translator.decide("Send a text.")
        assert isinstance(a, Clarify)
        assert translator.pending_sms is not None

    def test_unknown_phrasing_clarifies(self, translator):
        a = translator.decide("Render the duck in 4D.")
        assert isinstance(a, Clarify)


class TestTranslatorSlotFilling:
    def test_two_step_sms_slot_fill(self, translator):
        # First turn starts the pending-SMS state.
        first = translator.handle_user("Send a text.")
        assert "who" in first.lower()

        # User supplies the recipient.
        second = translator.handle_user("Lola.")
        assert "what" in second.lower() and "Lola" in second

        # User supplies the body. Translator now delegates.
        third = translator.handle_user("I'll be ten minutes late.")
        assert "Texted Lola" in third
        assert translator.pending_sms is None

    def test_cancel_clears_pending_state(self, translator):
        translator.handle_user("Send a text.")
        assert translator.pending_sms is not None
        reply = translator.handle_user("Nevermind.")
        assert translator.pending_sms is None
        assert "cancel" in reply.lower()


class TestTranslatorEffects:
    def test_handle_user_records_full_conversation(self, translator):
        translator.handle_user("Hi.")
        translator.handle_user("Log this: ok cool")
        speakers = [u.speaker for u in translator.conversation.utterances]
        assert speakers == ["user", "assistant", "user", "assistant"]

    def test_handle_user_passes_full_conversation_to_executor(self, translator, monkeypatch):
        """Per the design sketch: 'translator passes the full conversation
        history down to the executor.' Snapshot what the executor saw."""
        seen = {}
        original = translator.executor.execute

        def spy(req):
            # Snapshot at call time -- the conversation is mutated afterwards
            # when the assistant turn is appended.
            seen["utterances"] = list(req.conversation.utterances)
            seen["instruction"] = req.instruction
            return original(req)

        monkeypatch.setattr(translator.executor, "execute", spy)
        translator.handle_user("Hi.")  # handled directly, no executor call
        translator.handle_user("Log this: hello")  # delegates

        # At delegate time, conversation contains: user1, asst1, user2.
        speakers = [u.speaker for u in seen["utterances"]]
        assert speakers == ["user", "assistant", "user"]
        assert seen["utterances"][-1].text == "Log this: hello"
        assert seen["instruction"] == "Append to log: hello"
