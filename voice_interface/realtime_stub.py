"""Sketch of the OpenAI Realtime wiring.

This module is **intentionally not exercised by tests**. It documents the
swap path from the mock :class:`VoiceLayer` to the real OpenAI Realtime
API by mirroring the same surface (``user_says``, ``listen``, ``speak``)
on top of an event-driven WebSocket connection.

The shape matters more than the contents: anywhere the mock voice layer
is used in the orchestrator, this class drops in unchanged. The same is
true for the executor (real Claude Code CLI subprocess) and MCP servers
(real ``mcp`` package + JSON-RPC) -- each is a single-class swap.

To turn this on:

    pip install openai websockets
    export OPENAI_API_KEY=sk-...
    # then replace VoiceLayer() with RealtimeVoiceLayer() in your wiring.

The implementation is a stub -- it raises if anyone tries to use it
without the real dependencies installed.
"""

from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
from typing import Iterable


@dataclass
class RealtimeVoiceLayer:
    """Real OpenAI Realtime API voice layer (stub).

    Notes for implementation:

    * Connect to ``wss://api.openai.com/v1/realtime?model=gpt-realtime`` with
      the API key in the ``Authorization`` header.
    * Configure session with ``input_audio_format=pcm16`` (or g711_ulaw),
      ``output_audio_format=pcm16``, ``input_audio_transcription`` enabled,
      ``turn_detection={"type": "server_vad"}``.
    * Stream PCM frames from ``sounddevice.InputStream`` into
      ``input_audio_buffer.append`` events. The Realtime model does the
      VAD/turn-end and emits a ``conversation.item.input_audio_transcription
      .completed`` event with the cleaned transcript.
    * Forward each completed transcript to the orchestrator just as the
      mock layer's ``listen()`` does -- enqueue ``(cleaned, raw)``.
    * For ``speak(text)``: send a ``response.create`` with ``instructions``
      asking the model to speak the given text, route the resulting audio
      deltas to ``sounddevice.OutputStream``.
    * Cancel in-flight responses on barge-in (mic activity while speaking).

    None of that is implemented here -- just the surface.
    """

    pending: deque[str] = field(default_factory=deque)
    spoken: list[str] = field(default_factory=list)
    raw_transcripts: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        try:
            import openai  # noqa: F401
        except ImportError as e:
            raise RuntimeError(
                "RealtimeVoiceLayer requires `openai` and `websockets`; "
                "install them, then implement the connect path."
            ) from e
        raise NotImplementedError(
            "Implement WebSocket connect + audio I/O before constructing."
        )

    # Surface mirrors voice_interface.voice.VoiceLayer.
    def user_says(self, raw: str) -> None: ...
    def queue_user_turns(self, turns: Iterable[str]) -> None: ...
    def listen(self) -> tuple[str, str] | None: ...
    def speak(self, text: str) -> None: ...
    def has_pending(self) -> bool: ...
