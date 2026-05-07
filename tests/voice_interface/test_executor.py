"""Executor + rule-based planner tests.

These exercise the planner's intent matching and the executor's
plan-call-summarize loop without going through the translator. They pin
down the canonical "instruction grammar" the translator must emit.
"""

import pytest

from voice_interface.executor import Executor
from voice_interface.intents import RuleBasedPlanner
from voice_interface.mcp_servers import BrowserServer, FakePage, LogServer, SMSServer
from voice_interface.protocol import Conversation, ExecutorRequest


@pytest.fixture
def fixtures(tmp_path):
    log = LogServer(tmp_path / "voice.log")
    sms = SMSServer(contacts={"Lola": "+15555550100"})
    pages = {
        "https://shop.example/buy": FakePage(
            url="https://shop.example/buy",
            title="Shop",
            body="things",
            forms={"buy": {"item": "string", "qty": "int"}},
        )
    }
    browser = BrowserServer(pages)
    executor = Executor([log, sms, browser], RuleBasedPlanner())
    return tmp_path, log, sms, browser, executor


def _request(instruction: str) -> ExecutorRequest:
    return ExecutorRequest(Conversation(), instruction)


class TestExecutorLog:
    def test_log_intent_writes_and_summarizes(self, fixtures):
        tmp, log, _sms, _b, ex = fixtures
        resp = ex.execute(_request("Append to log: hello world"))
        assert resp.success
        assert "Logged" in resp.summary
        assert (tmp / "voice.log").read_text() == "hello world\n"

    def test_log_intent_strips_quotes_in_summary(self, fixtures):
        _t, _l, _s, _b, ex = fixtures
        resp = ex.execute(_request('Append to log: "hello world"'))
        # Both summary and stored text use the unquoted form.
        assert "hello world" in resp.summary


class TestExecutorSMS:
    def test_sms_send_intent(self, fixtures):
        _t, _l, sms, _b, ex = fixtures
        resp = ex.execute(_request("Send SMS to Lola saying I'll be home soon"))
        assert resp.success
        assert sms.outbox[0].body == "I'll be home soon"
        assert sms.outbox[0].to == "+15555550100"
        assert "Texted Lola" in resp.summary

    def test_sms_send_unknown_contact_surfaces_friendly_error(self, fixtures):
        _t, _l, sms, _b, ex = fixtures
        resp = ex.execute(_request("Send SMS to Stranger saying hi"))
        assert not resp.success
        assert "don" in resp.summary.lower()  # "don't have a number"
        assert sms.outbox == []

    def test_sms_inbox_intent_empty(self, fixtures):
        _t, _l, _s, _b, ex = fixtures
        resp = ex.execute(_request("Read SMS inbox"))
        assert resp.success
        assert "No new messages" in resp.summary

    def test_sms_inbox_intent_reads_messages(self, fixtures):
        _t, _l, sms, _b, ex = fixtures
        sms.deliver_inbound("Lola", "you up?")
        resp = ex.execute(_request("Read SMS inbox"))
        assert resp.success
        assert "Lola" in resp.summary and "you up?" in resp.summary


class TestExecutorBrowser:
    def test_shop_intent_runs_full_sequence(self, fixtures):
        _t, _l, _s, browser, ex = fixtures
        resp = ex.execute(
            _request("Shop for milk at https://shop.example/buy")
        )
        assert resp.success, resp.summary
        assert "Ordered milk" in resp.summary
        assert browser.submissions == [
            {
                "url": "https://shop.example/buy",
                "form": "buy",
                "fields": {"item": "milk", "qty": 1},
            }
        ]


class TestExecutorUnmatched:
    def test_unknown_instruction_returns_failure_with_message(self, fixtures):
        _t, _l, _s, _b, ex = fixtures
        resp = ex.execute(_request("Make me a sandwich"))
        assert not resp.success
        assert resp.tool_calls == []
        assert "not sure how to" in resp.summary.lower()
