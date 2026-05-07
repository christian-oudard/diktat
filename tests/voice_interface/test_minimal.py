"""The whole voice-agent loop, in tests.

These exercise the *mechanical* parts (Tool, step, dispatch) using a
canned LLM that returns predetermined responses. The intelligence layer
is intentionally not tested in Python -- that's a sub-agent / real-LLM
job, where the test is "spawn an agent with this prompt and tools, see
if it does the right thing."
"""

from dataclasses import dataclass
from pathlib import Path
from typing import Any

from voice_interface.minimal import LLM, Tool, ToolCall, step


# ---- a canned LLM that returns a queue of responses --------------------


@dataclass
class CannedLLM(LLM):
    responses: list  # of ToolCall | str

    def respond(self, conversation, tools):
        return self.responses.pop(0)


# ---- a log tool over a real file ---------------------------------------


def make_log_tool(path: Path) -> Tool:
    def write(args: dict[str, Any]) -> str:
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as f:
            f.write(args["text"].rstrip() + "\n")
        return f"Logged: {args['text']!r}"

    return Tool(
        name="log_write",
        description="Append a line to the user's voice log.",
        schema={
            "type": "object",
            "properties": {"text": {"type": "string"}},
            "required": ["text"],
        },
        fn=write,
    )


# ---- tests --------------------------------------------------------------


class TestMinimalLoop:
    def test_black_triangle(self, tmp_path):
        """User says something, LLM picks log_write, file gets the text."""
        log_path = tmp_path / "voice.log"
        tools = [make_log_tool(log_path)]
        llm = CannedLLM(responses=[
            ToolCall(name="log_write", args={"text": "hello world"})
        ])
        conversation: list = []

        reply = step("uh, log this hello world", conversation, llm, tools)

        assert log_path.read_text() == "hello world\n"
        assert "hello world" in reply
        assert [m["role"] for m in conversation] == ["user", "assistant"]

    def test_chat_only_no_tool(self, tmp_path):
        """LLM can also just talk back without calling a tool."""
        tools = [make_log_tool(tmp_path / "voice.log")]
        llm = CannedLLM(responses=["Sure, what's up?"])
        conversation: list = []

        reply = step("hi", conversation, llm, tools)

        assert reply == "Sure, what's up?"
        assert not (tmp_path / "voice.log").exists()

    def test_multiple_turns_grow_conversation(self, tmp_path):
        log_path = tmp_path / "voice.log"
        tools = [make_log_tool(log_path)]
        llm = CannedLLM(responses=[
            ToolCall(name="log_write", args={"text": "first"}),
            ToolCall(name="log_write", args={"text": "second"}),
        ])
        conversation: list = []

        step("log this first", conversation, llm, tools)
        step("log this second", conversation, llm, tools)

        assert log_path.read_text() == "first\nsecond\n"
        assert len(conversation) == 4  # 2 user + 2 assistant
