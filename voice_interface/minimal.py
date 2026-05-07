"""The whole voice-agent design, stripped to its essence.

    voice text  ->  LLM with tools  ->  tool fires  ->  side effect

The intelligence (intent, cleanup, anaphora, summarization) is all the
LLM's job. Python only owns the mechanical pieces:

    * Tool      -- a name + callable + schema, the unit the LLM picks.
    * step()    -- ask the LLM, dispatch the tool call (if any).
    * LLM       -- a Protocol so we can swap canned (tests) <-> Anthropic SDK.

That's the entire model. ~40 lines. Anything more should justify itself.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable, Protocol, Union


@dataclass(frozen=True)
class Tool:
    name: str
    description: str
    schema: dict[str, Any]
    fn: Callable[[dict[str, Any]], str]


@dataclass(frozen=True)
class ToolCall:
    name: str
    args: dict[str, Any]


# An LLM turn produces either a tool call (the agent wants to act) or a
# string (the agent is just talking back).
LLMResponse = Union[ToolCall, str]


class LLM(Protocol):
    def respond(
        self,
        conversation: list[dict[str, str]],
        tools: list[Tool],
    ) -> LLMResponse: ...


def step(
    user_text: str,
    conversation: list[dict[str, str]],
    llm: LLM,
    tools: list[Tool],
) -> str:
    """Run one voice turn. Mutates ``conversation``. Returns the agent reply."""
    conversation.append({"role": "user", "content": user_text})
    response = llm.respond(conversation, tools)
    if isinstance(response, str):
        reply = response
    else:
        tool = next(t for t in tools if t.name == response.name)
        result = tool.fn(response.args)
        reply = result
    conversation.append({"role": "assistant", "content": reply})
    return reply
