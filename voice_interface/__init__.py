"""Full voice interface: minimal model of a voice-driven agent stack.

The whole design boils down to one loop:

    voice text  ->  LLM with tools  ->  tool fires  ->  side effect

Intelligence (intent, cleanup, anaphora, summarization) lives in the LLM.
Python only owns the mechanical pieces:

    minimal.py      -- the Tool/step/LLM Protocol skeleton
    mcp_servers.py  -- in-process tool surfaces (Log, SMS) standing in for MCP
    harness.py      -- run a scenario through any LLM-shaped callable
    scenarios.py    -- scenarios as data, the brief given to a sub-agent
"""

from voice_interface.protocol import ToolCall, ToolResult, ToolSpec

__all__ = ["ToolCall", "ToolResult", "ToolSpec"]
