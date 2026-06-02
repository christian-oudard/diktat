# CLAUDE.md

## Overview

Voice dictation for Linux/Sway/Wayland. Moonshine ONNX tiny model, CPU only.
Go binaries built via nix `buildGoModule`. Models are bundled at build time
via fetchurl in `flake.nix`.

## Layout

- `cmd/whisper-dictation-daemon/` - daemon: keeps model loaded, toggles
  recording on SIGUSR1, transcribes, types via wtype.
- `cmd/whisper-dictation-toggle/` - sends SIGUSR1, starts daemon if absent.
- `cmd/whisper-dictation-repeat/` - re-types last transcription.
- `internal/` - shared packages: asr, audio, config, output.

## Runtime contract

The wrapped binaries see these environment variables (set by the nix wrapper):

- `ONNXRUNTIME_LIB` - absolute path to libonnxruntime.so
- `MOONSHINE_MODEL_DIR` - directory with `encoder.onnx`, `decoder.onnx`,
  `tokenizer.json`

External CLIs expected on PATH: `wtype`, `wl-copy`, `wl-paste`, `swaymsg`.
The wrapper prepends them.

## IPC files (in `/tmp`)

- `whisper-dictation-daemon.pid` - daemon PID
- `whisper-dictation-status` - Pango markup status string
- `whisper-dictation-last` - last transcribed text
- `whisper-dictation-daemon.log` - log

## Build

```
nix build
./result/bin/whisper-dictation-daemon
```

## Config

`~/.config/whisper-dictation/config.toml` (optional). Keys:

- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription

Vocabulary hints are not supported by Moonshine; the config key is ignored.
