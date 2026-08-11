# CLAUDE.md

## Overview

Voice dictation for Linux/Sway/Wayland. Default model is whisper-tiny.en, run
on a discrete GPU when there is one. Go binaries built via nix `buildGoModule`.
Models are downloaded on demand into the user's cache, not at build time.

## Layout

- `cmd/diktat/` - one binary, one file per subcommand (daemon, toggle,
  repeat, model, version, record, transcribe). `main.go` holds the dispatch
  table, which also backs the zsh completion in `completions/`. The nix build
  stamps the revision and commit date in via ldflags; the date uses a `T`
  rather than a space, since ldflags are joined on spaces.
- `daemon.go` - keeps the model loaded and warmed, toggles
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
- `GGML_BACKEND_DIR` - directory of ggml's backend .so files
- `VULKAN_LIB` - absolute path to libvulkan.so.1, dlopened by the GPU probe

Models are not part of the build; they are fetched at runtime into the user's
cache by `diktat model download`.

External CLIs expected on PATH: `wtype`, `wl-copy`, `wl-paste`, `swaymsg`.

## GPU

whisper.cpp is built with ggml's Vulkan backend, not CUDA: Vulkan is in the
binary cache, needs no unfree toolchain, and covers Intel and AMD as well as
NVIDIA. The encoder always runs on a padded 30 second window, so it costs the
same whatever the utterance length and dominates transcription. On a 2 second
utterance with whisper-base.en that is ~594ms on 22 CPU threads against ~23ms
on a laptop RTX 4070.

Whisper itself accepts the first device that is a GPU *or* an integrated GPU,
so on a hybrid laptop it will happily land on the Intel chip instead of the
discrete card. An iGPU shares memory bandwidth with the CPU it would be
replacing and is no clear win, so `diktat_discrete_gpu` in `internal/asr` walks
ggml's device list for a `GGML_BACKEND_DEVICE_TYPE_GPU`, skipping
`..._TYPE_IGPU`, and pins `gpu_device` to its index. The index is in whisper's
numbering, which counts both kinds, so it is not the same as ggml's. No
discrete device means CPU. `DIKTAT_GPU=0` forces CPU and `=1` takes whatever
whisper would have picked unaided. `diktat transcribe` prints the device it
chose, and so does the daemon's startup line.

Loading a model is not the same as being ready to use it: the Vulkan backend
defers compiling its shaders to the first encode, so the daemon runs one
throwaway transcription after every load, including a model switch. Without
that the cost lands on the first thing the user says.

The NVIDIA driver keeps compiled shaders in `~/.cache/nvidia/GLCache`, so that
warmup is ~30ms in the normal case and ~5.8s only when the cache is cold, which
means once per driver version rather than once per daemon start.

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

- `model` - model the daemon starts on, default `whisper-tiny.en`. `diktat
  model` switches the running daemon without writing here, so a restart comes
  back to a known model.
- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription

Vocabulary hints are not supported by Moonshine; the config key is ignored.
