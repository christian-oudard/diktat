"""Tests for the in-process mock MCP servers."""

import pytest

from voice_interface.mcp_servers import LogServer, SMSServer


class TestLogServer:
    def test_writes_line_to_file(self, tmp_path):
        log = LogServer(tmp_path / "voice.log")
        result = log.call_tool("write", {"text": "hello world"})
        assert result.ok
        assert (tmp_path / "voice.log").read_text() == "hello world\n"

    def test_appends_subsequent_writes(self, tmp_path):
        log = LogServer(tmp_path / "voice.log")
        log.call_tool("write", {"text": "first"})
        log.call_tool("write", {"text": "second"})
        assert (tmp_path / "voice.log").read_text() == "first\nsecond\n"

    def test_creates_parent_dirs(self, tmp_path):
        log = LogServer(tmp_path / "deep" / "nested" / "voice.log")
        log.call_tool("write", {"text": "hi"})
        assert (tmp_path / "deep" / "nested" / "voice.log").exists()

    def test_rejects_unknown_tool(self, tmp_path):
        log = LogServer(tmp_path / "voice.log")
        result = log.call_tool("delete", {})
        assert not result.ok and "unknown tool" in result.error

    def test_rejects_empty_text(self, tmp_path):
        log = LogServer(tmp_path / "voice.log")
        result = log.call_tool("write", {"text": ""})
        assert not result.ok

    def test_lists_tools(self, tmp_path):
        log = LogServer(tmp_path / "voice.log")
        tools = log.list_tools()
        assert [t.qualified for t in tools] == ["log.write"]


class TestSMSServer:
    @pytest.fixture
    def sms(self):
        return SMSServer(contacts={"Lola": "+15555550100", "Bob": "+15555550199"})

    def test_send_resolves_known_contact(self, sms):
        r = sms.call_tool("send", {"recipient": "Lola", "message": "I'll be home soon."})
        assert r.ok
        assert sms.outbox[0].to == "+15555550100"
        assert sms.outbox[0].body == "I'll be home soon."
        assert sms.outbox[0].contact == "Lola"

    def test_send_case_insensitive(self, sms):
        r = sms.call_tool("send", {"recipient": "lola", "message": "hi"})
        assert r.ok
        assert sms.outbox[-1].to == "+15555550100"

    def test_send_rejects_unknown_contact(self, sms):
        r = sms.call_tool("send", {"recipient": "Stranger", "message": "hello"})
        assert not r.ok
        assert "unknown contact" in r.error
        assert sms.outbox == []

    def test_send_passes_through_phone_numbers(self, sms):
        r = sms.call_tool("send", {"recipient": "+15558675309", "message": "yo"})
        assert r.ok
        assert sms.outbox[-1].to == "+15558675309"

    def test_send_rejects_empty_message(self, sms):
        r = sms.call_tool("send", {"recipient": "Lola", "message": ""})
        assert not r.ok and r.error == "empty message"

    def test_inbox_returns_messages_and_marks_read(self, sms):
        sms.deliver_inbound("Lola", "you up?")
        sms.deliver_inbound("Bob", "lunch?")
        r = sms.call_tool("inbox", {"unread_only": True})
        assert r.ok
        assert [m["from"] for m in r.data] == ["Lola", "Bob"]
        # Reading drains the inbox.
        r2 = sms.call_tool("inbox", {"unread_only": True})
        assert r2.data == []

    def test_lookup_contact(self, sms):
        r = sms.call_tool("lookup_contact", {"name": "Lola"})
        assert r.ok and r.data["number"] == "+15555550100"


