"""Executor: the lower / heavier agent.

In the real system this would be Claude Code CLI handed the conversation
plus an instruction. Here it's a small loop:

    plan = planner.plan(request, tool_catalog)
    results = [server.call_tool(tc.name, tc.args) for tc in plan]
    summary = planner.summarize(results)

The ``Planner`` interface is the only thing that changes between mock and
real: the rule-based planner in :mod:`voice_interface.intents` matches the
instruction with regexes; a real planner would prompt Claude with the
conversation and tool specs and parse ``tool_use`` blocks. Same shape.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from voice_interface.mcp_servers import MCPServer
from voice_interface.protocol import (
    ExecutorRequest,
    ExecutorResponse,
    ToolCall,
    ToolResult,
    ToolSpec,
)


@dataclass
class Plan:
    tool_calls: list[ToolCall]
    # Free-form scratch the planner uses when summarizing.
    notes: dict[str, object] | None = None


class Planner(Protocol):
    def plan(self, request: ExecutorRequest, tools: list[ToolSpec]) -> Plan: ...

    def summarize(
        self,
        request: ExecutorRequest,
        plan: Plan,
        results: list[ToolResult],
    ) -> str: ...


class Executor:
    def __init__(self, servers: list[MCPServer], planner: Planner):
        self.servers: dict[str, MCPServer] = {s.name: s for s in servers}
        self.planner = planner

    def tool_catalog(self) -> list[ToolSpec]:
        catalog: list[ToolSpec] = []
        for s in self.servers.values():
            catalog.extend(s.list_tools())
        return catalog

    def execute(self, request: ExecutorRequest) -> ExecutorResponse:
        plan = self.planner.plan(request, self.tool_catalog())
        results: list[ToolResult] = []
        for tc in plan.tool_calls:
            server = self.servers.get(tc.server)
            if server is None:
                results.append(
                    ToolResult(tc, ok=False, error=f"no such server: {tc.server}")
                )
                continue
            results.append(server.call_tool(tc.name, tc.args))
        success = bool(plan.tool_calls) and all(r.ok for r in results)
        summary = self.planner.summarize(request, plan, results)
        return ExecutorResponse(
            summary=summary,
            tool_calls=list(plan.tool_calls),
            tool_results=results,
            success=success,
            error=None if success else _first_error(results, plan),
        )


def _first_error(results: list[ToolResult], plan: Plan) -> str | None:
    if not plan.tool_calls:
        return "no plan"
    for r in results:
        if not r.ok:
            return r.error
    return None
