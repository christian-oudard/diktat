"""ASR backends.

Two interchangeable classes; both expose `transcribe(audio) -> str`
where `audio` is a 16 kHz mono float32 numpy array.
"""
from __future__ import annotations

import logging

import numpy as np

log = logging.getLogger(__name__)


class FasterWhisper:
    def __init__(self, model_name: str, vocabulary_hints: str | None = None):
        import huggingface_hub
        import huggingface_hub.errors
        from faster_whisper import WhisperModel

        repo = f"Systran/faster-whisper-{model_name}"
        try:
            huggingface_hub.snapshot_download(repo, local_files_only=True)
        except huggingface_hub.errors.LocalEntryNotFoundError:
            log.info("Model not cached, downloading...")
            huggingface_hub.snapshot_download(repo)

        try:
            self._model = WhisperModel(
                model_name, device="cuda", compute_type="float16", local_files_only=True
            )
            # Verify CUDA inference works (cublas may be missing even if model loads)
            self._model.model.encode(np.zeros((80, 100), dtype=np.float32))
        except (RuntimeError, ValueError, OSError):
            log.info("CUDA not available, using CPU")
            self._model = WhisperModel(
                model_name, device="cpu", compute_type="int8", local_files_only=True
            )
        self._vocabulary_hints = vocabulary_hints

    def transcribe(self, audio: np.ndarray) -> str:
        segments, _ = self._model.transcribe(
            audio,
            language="en",
            beam_size=5,
            vad_filter=True,
            initial_prompt=self._vocabulary_hints,
        )
        return "".join(s.text for s in segments).strip()


class MoonshineOnnx:
    def __init__(self, model_name: str, vocabulary_hints: str | None = None):
        from moonshine_onnx import MoonshineOnnxModel, load_tokenizer

        if vocabulary_hints:
            log.warning("moonshine-onnx does not support vocabulary_hints; ignoring")

        full = model_name if "/" in model_name else f"moonshine/{model_name}"
        self._model = MoonshineOnnxModel(model_name=full)
        self._tokenizer = load_tokenizer()

    def transcribe(self, audio: np.ndarray) -> str:
        tokens = self._model.generate(audio[None, :])
        return self._tokenizer.decode_batch(tokens)[0].strip()
