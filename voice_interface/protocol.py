"""Shared tool types.

Just enough to describe a tool surface and the calls/results that flow
across it. Anything else (dialogue policy, conversation shape, executor
plans) belongs to whoever is acting as the agent -- the LLM, not Python.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class ToolSpec:
    server: str
    name: str
    description: str
    schema: dict[str, Any] = field(default_factory=dict)

    @property
    def qualified(self) -> str:
        return f"{self.server}.{self.name}"


@dataclass(frozen=True)
class ToolCall:
    server: str
    name: str
    args: dict[str, Any]


@dataclass(frozen=True)
class ToolResult:
    call: ToolCall
    ok: bool
    data: Any = None
    error: str | None = None
