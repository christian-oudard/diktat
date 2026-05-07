"""The thing that pumps voice events through the stack.

In production this is a long-running loop on top of OpenAI Realtime: each
finalized user utterance triggers ``handle_user`` on the translator, the
translator's reply is spoken back. Inbound SMS and other async results
poke into the loop via ``announce``.
"""

from __future__ import annotations

from dataclasses import dataclass

from voice_interface.mcp_servers import SMSMessage, SMSServer
from voice_interface.translator import Translator
from voice_interface.voice import VoiceLayer


@dataclass
class Orchestrator:
    voice: VoiceLayer
    translator: Translator

    def step(self) -> bool:
        """Process at most one user turn. Returns False if nothing pending."""
        next_input = self.voice.listen()
        if next_input is None:
            return False
        cleaned, raw = next_input
        if not cleaned:
            self.voice.speak("I didn’t catch that.")
            return True
        response = self.translator.handle_user(cleaned, raw=raw)
        self.voice.speak(response)
        return True

    def drain(self) -> int:
        """Run until no more user turns are queued. Returns count handled."""
        n = 0
        while self.step():
            n += 1
        return n

    def announce(self, text: str) -> None:
        """Push an unsolicited assistant utterance (e.g. inbound SMS poke)."""
        from voice_interface.protocol import Utterance
        self.translator.conversation.append(
            Utterance("assistant", text, timestamp=self.translator.clock())
        )
        self.voice.speak(text)

    def watch_inbound_sms(self, sms: SMSServer) -> None:
        """Subscribe to inbound messages on an SMS server and announce them.

        The receive half of the design sketch's two SMS operations: the
        phone bridge surfaces a new message, the agent says it out loud.
        """

        def on_inbound(msg: SMSMessage) -> None:
            who = msg.contact or msg.from_ or "Unknown"
            self.announce(f"New message from {who}: {msg.body}")

        sms.subscribe_inbound(on_inbound)
