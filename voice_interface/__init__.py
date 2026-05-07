"""Full voice interface: model of a 4-piece voice agent stack.

Pieces (per the design sketch):
    User <-> VoiceLayer <-> Translator <-> Executor <-> MCP servers

Every external boundary (audio I/O, LLM calls, MCP tools) is behind an
interface so the same orchestrator can run with mocks (these tests) or with
real implementations (OpenAI Realtime, Haiku, Claude Code CLI, real MCP
servers) by swapping a single component.
"""

from voice_interface.protocol import (
    Clarify,
    Conversation,
    Delegate,
    Direct,
    ExecutorRequest,
    ExecutorResponse,
    ToolCall,
    ToolResult,
    ToolSpec,
    Utterance,
)

__all__ = [
    "Clarify",
    "Conversation",
    "Delegate",
    "Direct",
    "ExecutorRequest",
    "ExecutorResponse",
    "ToolCall",
    "ToolResult",
    "ToolSpec",
    "Utterance",
]
