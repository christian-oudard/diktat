# CLAUDE.md

## Overview

Voice dictation for Linux/Sway/Wayland. Moonshine ONNX tiny model, CPU only.
Go binaries built via nix `buildGoModule`. Models are fetched at build
time by a resumable curl download in `flake.nix`.

## Layout

- `cmd/diktat-daemon/` - daemon: keeps model loaded, toggles
  recording on SIGUSR1, transcribes, types via wtype.
- `cmd/diktat-toggle/` - sends SIGUSR1, starts daemon if absent.
- `cmd/diktat-repeat/` - re-types last transcription.
- `internal/` - shared packages: asr, audio, config, output.

## Runtime contract

The wrapped binaries see these environment variables (set by the nix wrapper):

- `ONNXRUNTIME_LIB` - absolute path to libonnxruntime.so
- `MOONSHINE_MODEL_DIR` - directory with `encoder.onnx`, `decoder.onnx`,
  `tokenizer.json`

External CLIs expected on PATH: `wtype`, `wl-copy`, `wl-paste`, `swaymsg`.
The wrapper prepends them.

## IPC files (in `/tmp`)

- `diktat-daemon.pid` - daemon PID
- `diktat-status` - Pango markup status string
- `diktat-last` - last transcribed text
- `diktat-daemon.log` - log

## Build

```
nix build
./result/bin/diktat-daemon
```

## Config

`~/.config/diktat/config.toml` (optional). Keys:

- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription

Vocabulary hints are not supported by Moonshine; the config key is ignored.
