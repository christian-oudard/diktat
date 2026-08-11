# CLAUDE.md

## Overview

Voice dictation for Linux/Sway/Wayland. Moonshine ONNX tiny model, CPU only.
Go binaries built via nix `buildGoModule`. Models are fetched at build
time by a resumable curl download in `flake.nix`.

## Layout

- `cmd/diktat/` - one binary, one file per subcommand (daemon, toggle,
  repeat, model, version, record, transcribe). `main.go` holds the dispatch
  table, which also backs the zsh completion in `completions/`. The nix build
  stamps the revision and commit date in via ldflags; the date uses a `T`
  rather than a space, since ldflags are joined on spaces.
- `daemon.go` - keeps the model loaded, toggles
  recording on SIGUSR1, transcribes, types via wtype. Runs for the whole
  session; it never starts recording by itself and never exits by itself.
  Signal handlers are installed before the model load so a toggle during
  startup is queued rather than killing the process. Recording is capped at
  60s, since memory grows with utterance length and the daemon is resident.
  The cap is enforced twice: a wall-clock timer in the daemon, and a sample
  count in `internal/audio` that does not trust the device's frame rate.
- `model.go` - lists, switches (via SIGHUP), or downloads models. Models stay
  resident once loaded, so switching back is instant.
- `internal/models` - the menu. Four entries, none bundled: everything is
  downloaded into `~/.cache/diktat/models`, so no model is a special case.
  Downloads are never implicit.
- `internal/asr` - `Backend` is the interface the daemon holds; `asr.Load`
  picks the implementation from the path. A directory is moonshine, whose
  layer count, KV head count and head dim are read off the decoder's ONNX
  input shapes rather than compiled in, so any size loads. A `.bin` is
  whisper, shelled out to `whisper-cli`.
- `internal/wav` - WAV read/write, split out from `internal/audio` so `asr`
  can write whisper's input file without pulling in malgo.
- `internal/` - shared packages: asr, audio, config, output.

## Runtime contract

The wrapped binaries see these environment variables (set by the nix wrapper):

- `ONNXRUNTIME_LIB` - absolute path to libonnxruntime.so

Models are not part of the build; they are fetched at runtime into the user's
cache by `diktat model download`.

External CLIs expected on PATH: `wtype`, `wl-copy`, `wl-paste`, `swaymsg`.

whisper.cpp is linked in via cgo rather than shelled out to, so its model
stays loaded. ggml loads each compute backend from a separate shared library
at runtime, which nothing finds by default from a Go binary, so the wrapper
sets `GGML_BACKEND_DIR` and `internal/asr` calls
`ggml_backend_load_all_from_path` with it. Without that ggml registers no
backend and aborts inside `whisper_init`.
The wrapper prepends them.

## IPC files (in `/tmp`)

- `diktat-daemon.pid` - daemon PID
- `diktat-status` - Pango markup status string
- `diktat-last` - last transcribed text
- `diktat-last.wav` - audio of the last capture, pre-normalization
- `diktat-model` - model directory currently loaded
- `diktat-daemon.log` - log

## Build

```
nix build
./result/bin/diktat model download whisper-tiny.en
./result/bin/diktat daemon
```

`nix build` only writes ./result; it puts nothing on PATH. To get `diktat`
itself on PATH, `nix profile add .`.

## Config

`~/.config/diktat/config.toml` (optional). Keys:

- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription

Vocabulary hints are not supported by Moonshine; the config key is ignored.
