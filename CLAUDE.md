# CLAUDE.md

## Overview

Voice dictation for Linux/Sway/Wayland. Default model is whisper-tiny.en, run
on a discrete GPU when there is one. The menu also carries parakeet, canary
and granite, which beat it on both accuracy and short-utterance latency; the
default moves once they have been used in anger. Go binaries built via nix `buildGoModule`.
Models are downloaded on demand into the user's cache, not at build time.
Speech recognition is transcribe.cpp throughout, linked in through its Go
bindings; there is no second engine.

## Layout

- `cmd/diktat/` - the shipped binary, one file per subcommand (daemon,
  toggle, repeat, model, version). `main.go` holds the dispatch
  table; the zsh completion in `completions/` reads the command list back out
  of `--help` rather than keeping a copy, since the copy drifted. The nix build
  stamps the revision and commit date in via ldflags; the date uses a `T`
  rather than a space, since ldflags are joined on spaces.
- `daemon.go` - keeps the model loaded and warmed, bounds the resident set
  (see Model cache below), toggles recording on SIGUSR1, transcribes, types
  via wtype. Runs for the whole
  session; it never starts recording by itself and never exits by itself.
  Signal handlers are installed before the model load so a toggle during
  startup is queued rather than killing the process. Recording is capped at
  60s, since memory grows with utterance length and the daemon is resident.
  The cap is enforced twice: a wall-clock timer in the daemon, and a sample
  count in `internal/audio` that does not trust the device's frame rate.
- `model.go` - lists the menu, or switches to an entry by number, by name or
  by path (via SIGHUP), fetching it first if the cache lacks it. There is no
  separate download verb: naming a model is the only reason to want one, and
  the prompt keeps the fetch from being silent.
- `cmd/transcribe/` - runs the daemon's pipeline over WAV files, for
  comparing models on fixed audio. The flake's `subPackages` builds only
  `cmd/diktat`, so this is never installed: it is `go run ./cmd/transcribe`
  in the devShell. Go rather than a script because it has to run the real
  pipeline, which a script would have to reimplement.
- `internal/models` - the menu. Thirteen entries, none bundled: everything is
  downloaded into `~/.cache/diktat/models` from the `handy-computer` GGUF
  repos, so no model is a special case. Downloads are never implicit. The
  menu carries a language set and a vocab flag per model, both hand-kept
  because the menu has to answer before a model is downloaded, and both
  checked against the library by a test for whatever is present. Entries are
  ordered by download size, which is also roughly the cost order, and the
  number in that listing is how models get selected. Whisper encodes a padded
  30 second window whatever was said, so on a 2 second utterance it costs the
  same as on a 30 second one, while every other family here encodes only what
  it was given; the whispers earn their place on languages and on vocabulary
  hints, which no other family in the library takes. Several entries are
  recent enough to have no published accuracy figure and are there to be
  tried, not because they are known good.
- `internal/asr` - one `Model` over transcribe.cpp. There is no backend
  interface any more: every model is a GGUF and the library reads the
  architecture out of it, so moonshine and whisper are not distinguishable
  here. Picks the discrete GPU when there is one.
- `internal/wav` - WAV read/write, split out from `internal/audio` so the
  offline tools can read a clip without pulling in malgo.
- `internal/` - shared packages: asr, audio, config, human, output. `human`
  renders sizes in binary units at one precision rule, since the menu, the
  download progress and the daemon's memory lines all print the same kind of
  number and were each rounding it differently.

## Runtime contract

libtranscribe is linked at build time and its backends are compiled in, so
the wrapper sets no library-path variables of its own beyond the audio and
Vulkan loaders it needs at runtime.

Models are not part of the build; they are fetched at runtime into the user's
cache by `diktat model <name>`, which asks before fetching.

External CLIs expected on PATH: `wtype`, `wl-copy`, `wl-paste`, `swaymsg`.

## GPU

transcribe.cpp is built with ggml's Vulkan backend, not CUDA: Vulkan is in the
binary cache, needs no unfree toolchain, and covers Intel and AMD as well as
NVIDIA. Whisper's encoder always runs on a padded 30 second window, so it costs
the same whatever the utterance length and dominates transcription. On a 2
second utterance with whisper-base.en that is ~594ms on 22 CPU threads against
~23ms on a laptop RTX 4070.

The library takes the first device that is a GPU *or* an integrated GPU, so on
a hybrid laptop it will happily land on the Intel chip instead of the discrete
card. An iGPU shares memory bandwidth with the CPU it would be replacing and is
no clear win, so `placement` in `internal/asr` walks `transcribe.Devices()` for
a `DeviceGPU`, skipping `DeviceIGPU`, and pins `LoadOptions.GPUDevice` to its
index. No discrete device means CPU. `DIKTAT_GPU=0` forces CPU and `=1` takes
whatever the library would have picked unaided. `cmd/transcribe` prints the
device it chose, and so does the daemon's startup line.

Loading a model is not the same as being ready to use it: the Vulkan backend
defers compiling its shaders to the first encode, so the daemon runs one
throwaway transcription after every load, including a model switch. Without
that the cost lands on the first thing the user says.

The NVIDIA driver keeps compiled shaders in `~/.cache/nvidia/GLCache`, so that
warmup is ~30ms in the normal case and ~5.8s only when the cache is cold, which
means once per driver version rather than once per daemon start.

## Model cache

Every model loaded stays resident, so switching back is instant. That needs a
ceiling now the models are large: the laptop GPU has 8 GB shared with the
desktop, and ggml's context and compute buffers cost more than the weights do
for a small model, so nvidia-smi shows ~1.4 GB resident for a 44 MB file.

`asr.Load` measures the cost by reading the device's free memory either side
of the load, which captures those buffers; a backend reporting no memory
falls back to the file size. `overBudget` in `daemon.go` picks what to drop,
oldest first, and never the model in use, so too small a budget degrades to
keeping exactly one model rather than to keeping none.

## IPC files (in `/tmp`)

- `diktat-daemon.pid` - daemon PID
- `diktat-status` - Pango markup status string
- `diktat-last` - last transcribed text
- `diktat-last.wav` - audio of the last capture, pre-normalization
- `diktat-model` - model file currently loaded
- `diktat-daemon.log` - log

## Build

```
nix build
./result/bin/diktat model whisper-tiny.en
./result/bin/diktat daemon
```

The Go bindings to transcribe.cpp live in that project's own tree and are
vendored here. `go mod vendor` needs the checkout beside this one, since
go.mod resolves them through a relative `replace`.

`nix build` only writes ./result; it puts nothing on PATH. To get `diktat`
itself on PATH, `nix profile add .`.

## Config

`~/.config/diktat/config.toml` (optional). Keys:

- `model` - model the daemon starts on before anything has been chosen,
  default `whisper-tiny.en`. `diktat model` records its choice in
  $XDG_STATE_HOME/diktat/model instead of writing here, since this file is
  hand-authored; that choice outranks this key, and deleting it restores
  this one.
- `model_cache_mb` - ceiling on what resident models hold together. 0 takes
  two thirds of the compute device's memory.
- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription

- `vocabulary_hints` - words the model would otherwise get wrong, passed as
  whisper's initial prompt. Only the whisper family takes them: it is the one
  architecture here with a prompt to condition on, which `asr.Model` probes
  for with `FEATURE_INITIAL_PROMPT` plus acceptance of the whisper run
  extension. The daemon logs when a loaded model cannot use them, since a
  list that is silently ignored looks like it is working.

Unknown keys are reported rather than ignored. TOML drops them silently,
which is how `vocabulary_hints` sat in a real config doing nothing from the
Python rewrite until it was wired back up.
