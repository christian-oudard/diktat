"""In-process mock MCP servers.

Each class implements the same minimal surface that a real MCP server
exposes: ``list_tools()`` and ``call_tool(name, args)``. The executor only
talks to this surface, so swapping in a real MCP client (subprocess + JSON-RPC,
or the official mcp-python client) is a one-class replacement.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable

from voice_interface.protocol import ToolCall, ToolResult, ToolSpec


class MCPServer:
    """Common surface for mock and real MCP servers."""

    name: str

    def list_tools(self) -> list[ToolSpec]:
        raise NotImplementedError

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        raise NotImplementedError


# ---------------------------------------------------------------------------
# Log server -- the "black triangle" target. Voice command -> append to file.
# ---------------------------------------------------------------------------


class LogServer(MCPServer):
    name = "log"

    def __init__(self, path: Path):
        self.path = Path(path)

    def list_tools(self) -> list[ToolSpec]:
        return [
            ToolSpec(
                server=self.name,
                name="write",
                description="Append a line to the user's voice log.",
                schema={"text": "string"},
            )
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


# ---------------------------------------------------------------------------
# SMS server -- the MVP target. Send + inbox.
# ---------------------------------------------------------------------------


@dataclass
class SMSMessage:
    to: str | None  # E.164-ish, None for inbound
    from_: str | None
    body: str
    contact: str | None = None  # display name if known


class SMSServer(MCPServer):
    """Mock SMS gateway with a contacts table and an inbox/outbox.

    In the real system, "send" hits an MCP messaging server (e.g., Twilio)
    and "inbox" reads new SMS surfaced from the phone -- exactly the two
    operations called out in the design sketch.
    """

    name = "sms"

    def __init__(self, contacts: dict[str, str] | None = None):
        # Map lowercase display name -> phone number
        self.contacts: dict[str, str] = {
            (k or "").strip().lower(): v for k, v in (contacts or {}).items()
        }
        self.outbox: list[SMSMessage] = []
        self.inbox: list[SMSMessage] = []
        self._inbound_listeners: list[Callable[[SMSMessage], None]] = []

    # Test/runtime helpers -------------------------------------------------

    def add_contact(self, name: str, number: str) -> None:
        self.contacts[name.strip().lower()] = number

    def subscribe_inbound(self, fn: Callable[[SMSMessage], None]) -> None:
        """Register a listener fired for each inbound SMS as it arrives.

        In production the phone bridge does this so the agent can surface
        new messages without the user having to ask -- the design sketch's
        'receive' operation.
        """
        self._inbound_listeners.append(fn)

    def deliver_inbound(self, from_name_or_number: str, body: str) -> None:
        """Simulate a phone surfacing an inbound SMS to the agent."""
        contact, number = self._resolve(from_name_or_number)
        msg = SMSMessage(to=None, from_=number, body=body, contact=contact)
        self.inbox.append(msg)
        for fn in self._inbound_listeners:
            fn(msg)

    def _resolve(self, who: str) -> tuple[str | None, str]:
        key = (who or "").strip().lower()
        if key in self.contacts:
            return who.strip(), self.contacts[key]
        # Looks like a number? Pass through.
        if any(ch.isdigit() for ch in who):
            return None, who.strip()
        # Unknown contact name. Caller decides what to do.
        return who.strip(), ""

    # MCP surface ----------------------------------------------------------

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
                return ToolResult(
                    call,
                    ok=False,
                    error=f"unknown contact: {recipient!r}",
                    data={"contact": contact},
                )
            self.outbox.append(
                SMSMessage(to=number, from_=None, body=message, contact=contact)
            )
            return ToolResult(call, ok=True, data={"to": number, "contact": contact})
        if name == "inbox":
            unread_only = bool(args.get("unread_only", True))
            messages = list(self.inbox)
            if unread_only:
                # In this mock we treat inbox-read as marking-read.
                self.inbox.clear()
            return ToolResult(
                call,
                ok=True,
                data=[
                    {"from": m.contact or m.from_, "body": m.body} for m in messages
                ],
            )
        if name == "lookup_contact":
            contact, number = self._resolve(args.get("name", ""))
            if not number:
                return ToolResult(call, ok=False, error="unknown contact")
            return ToolResult(call, ok=True, data={"contact": contact, "number": number})
        return ToolResult(call, ok=False, error=f"unknown tool: {name}")


# ---------------------------------------------------------------------------
# Browser server -- V2 stub, just enough to test multi-step shopping flow.
# ---------------------------------------------------------------------------


@dataclass
class FakePage:
    url: str
    title: str
    body: str
    forms: dict[str, dict[str, Any]] = field(default_factory=dict)


class BrowserServer(MCPServer):
    """A toy browser. Pages live in a dict keyed by URL."""

    name = "browser"

    def __init__(self, pages: dict[str, FakePage] | None = None):
        self.pages: dict[str, FakePage] = dict(pages or {})
        self.current: FakePage | None = None
        self.submissions: list[dict[str, Any]] = []

    def list_tools(self) -> list[ToolSpec]:
        return [
            ToolSpec(self.name, "navigate", "Go to a URL.", {"url": "string"}),
            ToolSpec(self.name, "read", "Read current page text.", {}),
            ToolSpec(self.name, "submit", "Submit a form on the current page.",
                     {"form": "string", "fields": "object"}),
        ]

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        call = ToolCall(self.name, name, args)
        if name == "navigate":
            url = args.get("url", "")
            page = self.pages.get(url)
            if not page:
                return ToolResult(call, ok=False, error=f"no such page: {url}")
            self.current = page
            return ToolResult(call, ok=True, data={"title": page.title, "url": url})
        if name == "read":
            if not self.current:
                return ToolResult(call, ok=False, error="no page loaded")
            return ToolResult(
                call, ok=True,
                data={"title": self.current.title, "body": self.current.body},
            )
        if name == "submit":
            if not self.current:
                return ToolResult(call, ok=False, error="no page loaded")
            form = args.get("form", "")
            if form not in self.current.forms:
                return ToolResult(call, ok=False, error=f"no form: {form}")
            self.submissions.append(
                {"url": self.current.url, "form": form, "fields": dict(args.get("fields", {}))}
            )
            return ToolResult(call, ok=True, data={"submitted": form})
        return ToolResult(call, ok=False, error=f"unknown tool: {name}")
