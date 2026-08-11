#!/usr/bin/env bash
# Transcribe the same WAVs with several ASR engines, to decide which one this
# project should use. Dev tool: the extra models are cached outside the nix
# build so they never enter the runtime closure.
#
#   $ ./scripts/compare-asr.sh recording.wav [more.wav ...]
#
# Capture material with `diktat-record out.wav`, or reuse what the daemon
# already saved at /tmp/diktat-last.wav after a bad transcription.
set -euo pipefail

cache="${XDG_CACHE_HOME:-$HOME/.cache}/diktat"
models="$cache/models"
mkdir -p "$models" "$cache/whisper"

fetch() { # url dest
    [ -s "$2" ] || curl -sL --fail --continue-at - -o "$2" "$1"
}

moonshine() { # size, shared with diktat-model
    local d="$models/moonshine-$1"
    mkdir -p "$d"
    local base=https://huggingface.co/UsefulSensors
    fetch "$base/moonshine/resolve/main/onnx/merged/$1/float/encoder_model.onnx" "$d/encoder.onnx"
    fetch "$base/moonshine/resolve/main/onnx/merged/$1/float/decoder_model_merged.onnx" "$d/decoder.onnx"
    fetch "$base/moonshine-$1/resolve/main/tokenizer.json" "$d/tokenizer.json"
    echo "$d"
}

whisper() { # size
    local f="$cache/whisper/ggml-$1.bin"
    fetch "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$1.bin" "$f"
    echo "$f"
}

tx=$(mktemp -d)/diktat-transcribe
trap 'rm -rf "$(dirname "$tx")"' EXIT
go build -o "$tx" ./cmd/diktat-transcribe

for size in tiny base; do
    echo "=== moonshine $size ==="
    MOONSHINE_MODEL_DIR=$(moonshine "$size") "$tx" "$@"
done

for size in tiny.en base.en; do
    echo "=== whisper $size ==="
    model=$(whisper "$size")
    for wav in "$@"; do
        text=$(nix run nixpkgs#whisper-cpp -- -m "$model" -f "$wav" -nt 2>/dev/null | tr -s ' \n' ' ')
        printf '%s  ->  "%s"\n' "$wav" "${text# }"
    done
done
