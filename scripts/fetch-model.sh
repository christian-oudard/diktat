#!/usr/bin/env bash
# Download a model into the cache, where a bare name given to diktat-model
# resolves.
#
#   $ ./scripts/fetch-model.sh moonshine-base
#   $ ./scripts/fetch-model.sh whisper-tiny.en
#   $ diktat-model whisper-tiny.en
set -euo pipefail

name="${1:?usage: fetch-model.sh <moonshine-tiny|moonshine-base|whisper-SIZE>}"
models="${XDG_CACHE_HOME:-$HOME/.cache}/diktat/models"
mkdir -p "$models"

fetch() { # url dest
    if [ -s "$2" ]; then
        echo "have $(basename "$2")" >&2
        return
    fi
    echo "fetching $(basename "$2")" >&2
    curl -# -L --fail --continue-at - -o "$2" "$1"
}

case "$name" in
moonshine-*)
    size="${name#moonshine-}"
    dir="$models/$name"
    mkdir -p "$dir"
    hf=https://huggingface.co/UsefulSensors
    fetch "$hf/moonshine/resolve/main/onnx/merged/$size/float/encoder_model.onnx" "$dir/encoder.onnx"
    fetch "$hf/moonshine/resolve/main/onnx/merged/$size/float/decoder_model_merged.onnx" "$dir/decoder.onnx"
    fetch "$hf/moonshine-$size/resolve/main/tokenizer.json" "$dir/tokenizer.json"
    echo "$dir"
    ;;
whisper-*)
    # A single ggml file rather than a directory; that is how the daemon tells
    # a whisper model from a moonshine one.
    size="${name#whisper-}"
    out="$models/$name.bin"
    fetch "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-$size.bin" "$out"
    echo "$out"
    ;;
*)
    echo "unknown model $name (want moonshine-* or whisper-*)" >&2
    exit 1
    ;;
esac
