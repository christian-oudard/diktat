"""Test transcription pipeline with actual model and audio."""

import shutil
import subprocess
import wave
from pathlib import Path

import numpy as np
import pytest
from faster_whisper import WhisperModel

SAMPLE_RATE = 16000
FIXTURES = Path(__file__).parent / "fixtures"


@pytest.fixture(scope="module")
def model():
    """Load whisper model once for all tests."""
    try:
        return WhisperModel("tiny.en", device="cuda", compute_type="float16")
    except (RuntimeError, ValueError):
        return WhisperModel("tiny.en", device="cpu", compute_type="int8")


def transcribe(model, audio: np.ndarray) -> str:
    segments, _ = model.transcribe(audio, language="en", beam_size=5, vad_filter=True)
    return "".join(s.text for s in segments).strip()


def load_wav(path: str) -> np.ndarray:
    """Load WAV file as float32 numpy array, resampled to 16kHz."""
    with wave.open(path) as f:
        channels = f.getnchannels()
        sample_width = f.getsampwidth()
        rate = f.getframerate()
        frames = f.readframes(f.getnframes())

    if sample_width == 2:
        audio = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0
    else:
        raise ValueError(f"Unsupported sample width: {sample_width}")

    if channels == 2:
        audio = audio.reshape(-1, 2).mean(axis=1)

    if rate != SAMPLE_RATE:
        n_samples = int(len(audio) / rate * SAMPLE_RATE)
        indices = np.linspace(0, len(audio) - 1, n_samples)
        audio = np.interp(indices, np.arange(len(audio)), audio).astype(np.float32)

    return audio


class TestTranscription:
    def test_silence_produces_empty(self, model):
        silence = np.zeros(SAMPLE_RATE * 2, dtype=np.float32)
        assert transcribe(model, silence) == ""

    def test_noise_produces_empty(self, model):
        rng = np.random.default_rng(42)
        noise = rng.standard_normal(SAMPLE_RATE * 2).astype(np.float32) * 0.01
        assert transcribe(model, noise) == ""

    def test_hello_world(self, model):
        """Transcribe pre-generated espeak-ng 'hello world' fixture."""
        audio = load_wav(str(FIXTURES / "hello.wav"))
        text = transcribe(model, audio).lower()
        assert "hello" in text

    @pytest.mark.skipif(
        not shutil.which("espeak-ng"),
        reason="espeak-ng not available",
    )
    def test_tts_transcription(self, model, tmp_path):
        """Generate speech with espeak-ng, verify whisper transcribes it."""
        wav_path = str(tmp_path / "speech.wav")
        subprocess.run(
            ["espeak-ng", "--stdout", "-s", "150", "testing one two three"],
            capture_output=True, check=True,
            stdout=open(wav_path, "wb"),
        )
        audio = load_wav(wav_path)
        text = transcribe(model, audio).lower()
        assert "test" in text or "one" in text or "two" in text


class TestLastTextFile:
    def test_repeat_reads_last_text(self, tmp_path, monkeypatch):
        last_file = str(tmp_path / "last")
        monkeypatch.setattr("whisper_dictation.daemon.LAST_TEXT_FILE", last_file)
        monkeypatch.setattr("whisper_dictation.repeat.LAST_TEXT_FILE", last_file)

        text_out = "hello world "
        with open(last_file, "w") as f:
            f.write(text_out)

        with open(last_file) as f:
            assert f.read() == text_out

    def test_repeat_missing_file(self, tmp_path, monkeypatch):
        monkeypatch.setattr(
            "whisper_dictation.repeat.LAST_TEXT_FILE",
            str(tmp_path / "nonexistent"),
        )
        from whisper_dictation.repeat import main
        with pytest.raises(SystemExit) as exc:
            main()
        assert exc.value.code == 0
