#!/usr/bin/env python3
"""Whisper daemon - keeps model loaded, toggles recording on signal."""

import atexit
import json
import logging
import os
import signal
import sys
import time
import tomllib
from datetime import datetime, timezone
from pathlib import Path

from .text_output import type_text

LOG_FILE = "/tmp/whisper-dictation-daemon.log"
handlers: list[logging.Handler] = [logging.StreamHandler()]
try:
    handlers.append(logging.FileHandler(LOG_FILE, mode="w"))
except OSError:
    pass
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(message)s",
    datefmt="%H:%M:%S",
    handlers=handlers,
)
log = logging.getLogger(__name__)

MODEL = "tiny.en"
SAMPLE_RATE = 16000
PID_FILE = "/tmp/whisper-dictation-daemon.pid"
STATUS_FILE = "/tmp/whisper-dictation-status"
LAST_TEXT_FILE = "/tmp/whisper-dictation-last"
CONFIG_PATH = Path.home() / ".config" / "whisper-dictation" / "config.toml"
STATUS_LOADING = '<span color="#fabd2f">● LOAD</span>'
STATUS_REC = '<span color="#fb4934">● REC</span>'
STATUS_TX = '<span color="#458588">● TX</span>'
IDLE_TIMEOUT = 15 * 60  # 15 minutes


def set_status(status: str):
    try:
        with open(STATUS_FILE, "w") as f:
            f.write(status)
    except OSError:
        pass


def cleanup():
    for f in (PID_FILE, STATUS_FILE):
        try:
            os.unlink(f)
        except OSError:
            pass


def main():
    atexit.register(cleanup)
    # Write PID and show loading status before heavy imports
    try:
        with open(PID_FILE, "w") as f:
            f.write(str(os.getpid()))
        set_status(STATUS_LOADING)
    except OSError as e:
        log.error(f"Cannot write to /tmp: {e}")
        sys.exit(1)

    # Load config
    history_file = None
    vocabulary_hints = None
    try:
        with open(CONFIG_PATH, "rb") as f:
            config = tomllib.load(f)
        hf = config.get("history_file")
        if hf:
            history_file = Path(hf).expanduser()
        vocabulary_hints = config.get("vocabulary_hints")
    except (FileNotFoundError, tomllib.TOMLDecodeError):
        pass
    if vocabulary_hints:
        log.info(f"Vocabulary hints: {vocabulary_hints.strip()}")

    # Heavy imports
    import huggingface_hub
    import huggingface_hub.errors
    import numpy as np
    import sounddevice as sd
    from faster_whisper import WhisperModel

    log.info(f"Loading {MODEL}...")
    # Ensure model is cached locally (downloads on first run)
    local_only = True
    try:
        huggingface_hub.snapshot_download(f"Systran/faster-whisper-{MODEL}", local_files_only=True)
    except huggingface_hub.errors.LocalEntryNotFoundError:
        log.info("Model not cached, downloading...")
        try:
            huggingface_hub.snapshot_download(f"Systran/faster-whisper-{MODEL}")
        except Exception as e:
            log.error(f"Failed to download model: {e}")
            sys.exit(1)
    try:
        model = WhisperModel(MODEL, device="cuda", compute_type="float16", local_files_only=local_only)
        # Verify CUDA inference works (cublas may be missing even if model loads)
        model.model.encode(np.zeros((80, 100), dtype=np.float32))
    except (RuntimeError, ValueError, OSError):
        log.info("CUDA not available, using CPU")
        model = WhisperModel(MODEL, device="cpu", compute_type="int8", local_files_only=local_only)
    log.info("Model loaded.")

    # State
    recording = False
    audio_chunks = []

    def audio_callback(indata, frames, time_info, status):
        if recording:
            audio_chunks.append(indata[:, 0].copy())

    # Pre-create stream to eliminate device init latency on toggle
    stream = sd.InputStream(
        samplerate=SAMPLE_RATE,
        channels=1,
        dtype=np.float32,
        callback=audio_callback,
    )

    def start_recording():
        nonlocal recording, audio_chunks
        signal.alarm(0)  # Cancel any pending timeout
        audio_chunks = []
        stream.start()
        recording = True
        set_status(STATUS_REC)
        log.info("Recording...")

    def stop_recording():
        nonlocal recording
        recording = False
        stream.stop()

        if not audio_chunks:
            set_status("")
            signal.alarm(IDLE_TIMEOUT)
            log.info("No audio.")
            return

        set_status(STATUS_TX)

        audio = np.concatenate(audio_chunks)
        log.info(f"Transcribing {len(audio)/SAMPLE_RATE:.1f}s...")

        t0 = time.monotonic()
        segments, _ = model.transcribe(audio, language="en", beam_size=5, vad_filter=True, initial_prompt=vocabulary_hints)
        text = "".join(s.text for s in segments).strip()
        log.info(f"Transcribed in {time.monotonic()-t0:.2f}s")

        if text:
            text_out = text + " "
            try:
                with open(LAST_TEXT_FILE, "w") as f:
                    f.write(text_out)
            except OSError:
                pass
            if history_file:
                try:
                    history_file.parent.mkdir(parents=True, exist_ok=True)
                    with open(history_file, "a") as f:
                        json.dump({
                            "ts": datetime.now(timezone.utc).isoformat(),
                            "text": text,
                        }, f)
                        f.write("\n")
                except OSError:
                    pass
            type_text(text_out)

        set_status("")
        signal.alarm(IDLE_TIMEOUT)

    def toggle(sig, frame):
        nonlocal recording
        if recording:
            stop_recording()
        else:
            start_recording()

    def shutdown(sig, frame):
        nonlocal recording
        if recording:
            stop_recording()
        stream.stop()
        stream.close()
        log.info("Daemon stopped.")
        sys.exit(0)

    def idle_timeout(sig, frame):
        if not recording:
            log.info("Idle timeout, shutting down.")
            shutdown(sig, frame)
        # If recording, ignore - alarm will be reset when recording stops

    signal.signal(signal.SIGUSR1, toggle)
    signal.signal(signal.SIGTERM, shutdown)
    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGALRM, idle_timeout)

    # Start recording immediately
    start_recording()

    # Wait forever
    while True:
        signal.pause()


if __name__ == "__main__":
    main()
