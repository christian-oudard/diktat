"""End-to-end scenarios for the full voice interface.

Each scenario wires the full stack -- voice layer, translator, executor,
MCP servers -- and drives it from queued raw user transcripts. These are
the milestones from the design sketch:

    * Black triangle -- voice command writes a log file via MCP.
    * MVP           -- text-message conversation by voice.
    * V2            -- multi-step browser/shopping task.

Plus a handful of edge cases the design hand-waves over (cancellation,
unknown contacts, async inbound SMS, slot-fill).
"""

from pathlib import Path

import pytest

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


@pytest.fixture
def stack(tmp_path: Path):
    """Wire the full mock stack."""
    log = LogServer(tmp_path / "voice.log")
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
    return {
        "tmp": tmp_path,
        "log": log,
        "sms": sms,
        "browser": browser,
        "executor": executor,
        "translator": translator,
        "voice": voice,
        "orch": orch,
    }


# ---- Black triangle -----------------------------------------------------


class TestBlackTriangle:
    """Voice command -> MCP tool execution. The smallest end-to-end loop."""

    def test_voice_command_writes_log_file(self, stack):
        stack["voice"].user_says("uh, log this hello world")
        stack["orch"].drain()

        # The log file received the cleaned text.
        assert (stack["tmp"] / "voice.log").read_text() == "hello world\n"
        # The user heard a confirmation.
        assert any("Logged" in s for s in stack["voice"].spoken)

    def test_multiple_log_entries_in_one_session(self, stack):
        stack["voice"].queue_user_turns([
            "log this: bought milk",
            "log this: paid rent",
            "save: dinner with Lola Saturday",
        ])
        stack["orch"].drain()

        lines = (stack["tmp"] / "voice.log").read_text().splitlines()
        assert lines == [
            "bought milk",
            "paid rent",
            "dinner with Lola Saturday",
        ]


# ---- MVP: text conversation by voice -----------------------------------


class TestMVPTextConversation:
    """The MVP milestone: hold a text-message conversation entirely by voice,
    with clean grammar and exactly the intended message body."""

    def test_one_shot_text_to_lola(self, stack):
        stack["voice"].user_says(
            "uh, tell Lola I'll be like ten minutes late or so"
        )
        stack["orch"].drain()

        outbox = stack["sms"].outbox
        assert len(outbox) == 1
        # Exactly the intended message -- no fillers, no "or so".
        assert outbox[0].body == "I'll be ten minutes late"
        assert outbox[0].to == "+15555550100"
        assert any("Texted Lola" in s for s in stack["voice"].spoken)

    def test_slot_filled_text_conversation(self, stack):
        # User starts vague, fills in the slots over multiple turns.
        stack["voice"].queue_user_turns([
            "send a text",
            "Lola",
            "I'll be home soon",
        ])
        stack["orch"].drain()

        assert stack["sms"].outbox[0].body == "I'll be home soon"
        assert stack["sms"].outbox[0].to == "+15555550100"
        # The dialogue had three clarifying/confirming responses, in order.
        spoken = stack["voice"].spoken
        assert "who" in spoken[0].lower()
        assert "Lola" in spoken[1]
        assert "Texted Lola" in spoken[2]

    def test_inbox_check_with_two_messages(self, stack):
        stack["sms"].deliver_inbound("Lola", "you up?")
        stack["sms"].deliver_inbound("Bob", "lunch?")
        stack["voice"].user_says("any new messages")
        stack["orch"].drain()

        confirmation = stack["voice"].spoken[-1]
        assert "2 new messages" in confirmation
        assert "Lola" in confirmation and "Bob" in confirmation
        # Reading the inbox drains it.
        assert stack["sms"].inbox == []

    def test_full_back_and_forth(self, stack):
        """A realistic sequence: receive, act, send, log."""
        stack["sms"].deliver_inbound("Lola", "what's for dinner")
        stack["voice"].queue_user_turns([
            "any messages?",
            "tell Lola: tacos",
            "log this: tacos for dinner",
        ])
        stack["orch"].drain()

        assert any("Lola" in s and "what's for dinner" in s
                   for s in stack["voice"].spoken)
        assert stack["sms"].outbox[0].body == "tacos"
        assert (stack["tmp"] / "voice.log").read_text().strip() == "tacos for dinner"


# ---- Edge cases the design hand-waves over -----------------------------


class TestDraftConfirmRevise:
    """Draft mode: user crafts a text, can revise before sending. Mirrors the
    'exactly what was intended' goal -- give the user a chance to refine."""

    def test_draft_then_confirm_sends(self, stack):
        stack["voice"].queue_user_turns([
            "draft a text to Lola: I'll be home soon",
            "send it",
        ])
        stack["orch"].drain()

        assert stack["sms"].outbox[0].body == "I'll be home soon"
        # The agent showed the draft before sending.
        assert any("I'll be home soon" in s and "Send" in s
                   for s in stack["voice"].spoken)

    def test_draft_revise_then_send(self, stack):
        stack["voice"].queue_user_turns([
            "draft a text to Lola: I'll be home soon",
            "make it: I'll be home in 20 minutes",
            "yes",
        ])
        stack["orch"].drain()

        # Only one outbound message, with the revised body.
        assert len(stack["sms"].outbox) == 1
        assert stack["sms"].outbox[0].body == "I'll be home in 20 minutes"

    def test_draft_reword_by_just_speaking_new_body(self, stack):
        """If the user keeps talking instead of saying 'make it...', treat the
        turn as a new message body. Optimizes for low friction."""
        stack["voice"].queue_user_turns([
            "draft a text to Lola: dinner at six",
            "dinner at seven",
            "send it",
        ])
        stack["orch"].drain()

        # Cleanup capitalizes the first letter; SMS doesn't care.
        assert stack["sms"].outbox[0].body == "Dinner at seven"

    def test_draft_cancel_does_not_send(self, stack):
        stack["voice"].queue_user_turns([
            "draft a text to Lola: hi",
            "nevermind",
        ])
        stack["orch"].drain()

        assert stack["sms"].outbox == []
        assert stack["translator"].pending_sms is None

    def test_draft_to_someone_then_supply_body(self, stack):
        stack["voice"].queue_user_turns([
            "draft a text to Lola",       # missing body
            "be home in five",            # body fills slot, then confirm prompt
            "yes",                        # confirm
        ])
        stack["orch"].drain()

        assert stack["sms"].outbox[0].body == "Be home in five"

    def test_bare_draft_fills_both_slots_then_confirms(self, stack):
        stack["voice"].queue_user_turns([
            "draft a text",
            "Lola",
            "see you tonight",
            "yes",
        ])
        stack["orch"].drain()

        assert stack["sms"].outbox[0].body == "See you tonight"
        assert stack["sms"].outbox[0].to == "+15555550100"


class TestEdgeCases:
    def test_unknown_contact_doesnt_silently_send(self, stack):
        stack["voice"].user_says("text Stranger: hello")
        stack["orch"].drain()

        assert stack["sms"].outbox == []
        # User is told about it.
        last = stack["voice"].spoken[-1]
        assert "Stranger" in last
        assert "don" in last.lower()  # "don't have a number"

    def test_user_can_cancel_pending_text(self, stack):
        stack["voice"].queue_user_turns(["send a text", "nevermind"])
        stack["orch"].drain()

        assert stack["sms"].outbox == []
        assert stack["translator"].pending_sms is None
        assert any("cancel" in s.lower() for s in stack["voice"].spoken)

    def test_unrecognized_request_asks_for_rephrase(self, stack):
        stack["voice"].user_says("Render the duck in 4D.")
        stack["orch"].drain()

        last = stack["voice"].spoken[-1]
        assert "another way" in last.lower() or "not sure" in last.lower()

    def test_inbound_sms_can_be_announced(self, stack):
        """Async inbound message poked into the loop by some other process
        (in production, the phone bridge surfaces inbound SMS to the agent)."""
        stack["orch"].announce("Lola says: you up?")
        # The user heard it.
        assert stack["voice"].spoken == ["Lola says: you up?"]
        # And it's in the conversation transcript.
        last = stack["translator"].conversation.utterances[-1]
        assert last.speaker == "assistant"
        assert "Lola says" in last.text

    def test_subscribed_orchestrator_auto_announces_inbound_sms(self, stack):
        """Wire the orchestrator to the SMS server -- now inbound messages
        surface automatically without an explicit ``announce`` call."""
        stack["orch"].watch_inbound_sms(stack["sms"])
        stack["sms"].deliver_inbound("Lola", "you up?")

        assert any(
            "Lola" in s and "you up?" in s for s in stack["voice"].spoken
        )

    def test_auto_announce_then_user_can_reply(self, stack):
        """Realistic flow: phone delivers a text, agent reads it aloud, user
        dictates a reply, agent sends it."""
        stack["orch"].watch_inbound_sms(stack["sms"])
        stack["sms"].deliver_inbound("Lola", "what's for dinner?")

        # User replies via voice.
        stack["voice"].user_says("tell Lola: tacos")
        stack["orch"].drain()

        assert stack["sms"].outbox[0].body == "tacos"
        assert stack["sms"].outbox[0].to == "+15555550100"
        # The transcript shows: announcement, user, assistant.
        speakers = [u.speaker for u in stack["translator"].conversation.utterances]
        # Last three: assistant (announce), user, assistant (confirmation).
        assert speakers[-3:] == ["assistant", "user", "assistant"]


# ---- V2: multi-step browser flow ----------------------------------------


class TestV2Shopping:
    def test_one_turn_shopping(self, stack):
        stack["voice"].user_says("buy me milk on amazon")
        stack["orch"].drain()

        assert stack["browser"].submissions == [
            {
                "url": "https://shop.example/buy",
                "form": "buy",
                "fields": {"item": "milk", "qty": 1},
            }
        ]
        assert any("Ordered milk" in s for s in stack["voice"].spoken)

    def test_unknown_vendor_handled_gracefully(self, stack):
        stack["voice"].user_says("buy me bread on weirdmart")
        stack["orch"].drain()

        assert stack["browser"].submissions == []
        last = stack["voice"].spoken[-1]
        assert "weirdmart" in last.lower()
