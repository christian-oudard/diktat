"""Test the harness mechanics (prompt build, parse, dispatch, check).

Uses a canned LLM. The intelligence layer is exercised separately by
spawning a real sub-agent against the same scenarios -- see the
session transcript in the project history for examples.
"""

from voice_interface.harness import build_prompt, parse_reply, run
from voice_interface.scenarios import SCENARIOS


def _get(name):
    return next(s for s in SCENARIOS if s.name == name)


class TestPromptBuild:
    def test_includes_contacts_when_present(self):
        prompt = build_prompt(_get("anaphora-her-after-inbound"))
        assert "Lola: +15555550100" in prompt
        assert "what's for dinner?" in prompt
        assert "tell her tacos" in prompt

    def test_omits_empty_sections(self):
        prompt = build_prompt(_get("black-triangle"))
        assert "CONTACTS:" not in prompt
        assert "CONVERSATION SO FAR:" not in prompt
        assert "log_write" in prompt


class TestParseReply:
    def test_plain_json(self):
        out = parse_reply('{"tool": "log_write", "args": {"text": "hi"}}')
        assert out == {"tool": "log_write", "args": {"text": "hi"}}

    def test_strips_markdown_fences(self):
        out = parse_reply('```json\n{"say": "hello"}\n```')
        assert out == {"say": "hello"}

    def test_handles_surrounding_prose(self):
        out = parse_reply('Sure! {"say": "hi"} done.')
        assert out == {"say": "hi"}


class TestRunWithCannedLLM:
    def test_black_triangle_passes(self, tmp_path):
        result = run(
            _get("black-triangle"),
            llm=lambda prompt: '{"tool": "log_write", "args": {"text": "hello world"}}',
            log_path=tmp_path / "voice.log",
        )
        assert result["ok"], result["error"]
        assert "hello world" in result["world"]["log"]

    def test_anaphora_passes_when_agent_resolves_correctly(self, tmp_path):
        result = run(
            _get("anaphora-her-after-inbound"),
            llm=lambda prompt: (
                '{"tool": "sms_send", "args": '
                '{"recipient": "Lola", "message": "tacos"}}'
            ),
            log_path=tmp_path / "voice.log",
        )
        assert result["ok"], result["error"]
        assert result["world"]["sms"][0].to == "+15555550100"

    def test_anaphora_fails_when_agent_keeps_pronoun(self, tmp_path):
        """If the agent doesn't resolve 'her', the SMS server rejects it
        and the scenario check sees an empty outbox."""
        result = run(
            _get("anaphora-her-after-inbound"),
            llm=lambda prompt: (
                '{"tool": "sms_send", "args": '
                '{"recipient": "her", "message": "tacos"}}'
            ),
            log_path=tmp_path / "voice.log",
        )
        assert not result["ok"]
        assert "no SMS to" in (result["error"] or "")

    def test_chitchat_passes_with_say(self, tmp_path):
        result = run(
            _get("chitchat-no-tool"),
            llm=lambda prompt: '{"say": "Hi, what can I do?"}',
            log_path=tmp_path / "voice.log",
        )
        assert result["ok"], result["error"]

    def test_unknown_contact_passes_when_agent_refuses(self, tmp_path):
        result = run(
            _get("unknown-contact"),
            llm=lambda prompt: (
                '{"say": "I don\'t have a number for Stranger. '
                'Want to add one?"}'
            ),
            log_path=tmp_path / "voice.log",
        )
        assert result["ok"], result["error"]
