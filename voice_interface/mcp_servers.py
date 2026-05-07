"""In-process tool surfaces that mock real MCP servers.

Each class exposes the minimal MCP surface (``list_tools`` and
``call_tool``) so it can stand in for a real MCP server during testing.
The state is in memory, so tests inspect ``log.path`` or ``sms.outbox``
to assert what the agent did.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

from voice_interface.protocol import ToolCall, ToolResult, ToolSpec


class MCPServer:
    name: str

    def list_tools(self) -> list[ToolSpec]:
        raise NotImplementedError

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        raise NotImplementedError


# ---- Log: the black-triangle target ------------------------------------


class LogServer(MCPServer):
    name = "log"

    def __init__(self, path: Path):
        self.path = Path(path)

    def list_tools(self) -> list[ToolSpec]:
        return [
            ToolSpec(self.name, "write",
                     "Append a line to the user's voice log.",
                     {"text": "string"})
        ]

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        call = ToolCall(self.name, name, args)
        if name != "write":
            return ToolResult(call, ok=False, error=f"unknown tool: {name}")
        text = args.get("text")
        if not isinstance(text, str) or not text:
            return ToolResult(call, ok=False, error="missing text")
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as f:
            f.write(text.rstrip("\n") + "\n")
        return ToolResult(call, ok=True, data={"appended": text})


# ---- SMS: send + inbox -------------------------------------------------


@dataclass
class SMSMessage:
    to: str | None
    from_: str | None
    body: str
    contact: str | None = None


class SMSServer(MCPServer):
    name = "sms"

    def __init__(self, contacts: dict[str, str] | None = None):
        self.contacts: dict[str, str] = {
            (k or "").strip().lower(): v for k, v in (contacts or {}).items()
        }
        self.outbox: list[SMSMessage] = []
        self.inbox: list[SMSMessage] = []
        self._inbound_listeners: list[Callable[[SMSMessage], None]] = []

    def add_contact(self, name: str, number: str) -> None:
        self.contacts[name.strip().lower()] = number

    def subscribe_inbound(self, fn: Callable[[SMSMessage], None]) -> None:
        self._inbound_listeners.append(fn)

    def deliver_inbound(self, from_name_or_number: str, body: str) -> None:
        contact, number = self._resolve(from_name_or_number)
        msg = SMSMessage(to=None, from_=number, body=body, contact=contact)
        self.inbox.append(msg)
        for fn in self._inbound_listeners:
            fn(msg)

    def _resolve(self, who: str) -> tuple[str | None, str]:
        key = (who or "").strip().lower()
        if key in self.contacts:
            return who.strip(), self.contacts[key]
        if any(ch.isdigit() for ch in who):
            return None, who.strip()
        return who.strip(), ""

    def list_tools(self) -> list[ToolSpec]:
        return [
            ToolSpec(self.name, "send", "Send an SMS to a contact or number.",
                     {"recipient": "string", "message": "string"}),
            ToolSpec(self.name, "inbox", "List messages received since last check.",
                     {"unread_only": "bool?"}),
            ToolSpec(self.name, "lookup_contact", "Resolve a name to a phone number.",
                     {"name": "string"}),
        ]

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        call = ToolCall(self.name, name, args)
        if name == "send":
            recipient = args.get("recipient", "")
            message = args.get("message", "")
            if not message:
                return ToolResult(call, ok=False, error="empty message")
            contact, number = self._resolve(recipient)
            if not number:
                return ToolResult(call, ok=False,
                                  error=f"unknown contact: {recipient!r}",
                                  data={"contact": contact})
            self.outbox.append(
                SMSMessage(to=number, from_=None, body=message, contact=contact)
            )
            return ToolResult(call, ok=True, data={"to": number, "contact": contact})
        if name == "inbox":
            unread_only = bool(args.get("unread_only", True))
            messages = list(self.inbox)
            if unread_only:
                self.inbox.clear()
            return ToolResult(
                call, ok=True,
                data=[{"from": m.contact or m.from_, "body": m.body} for m in messages],
            )
        if name == "lookup_contact":
            contact, number = self._resolve(args.get("name", ""))
            if not number:
                return ToolResult(call, ok=False, error="unknown contact")
            return ToolResult(call, ok=True, data={"contact": contact, "number": number})
        return ToolResult(call, ok=False, error=f"unknown tool: {name}")
