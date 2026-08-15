# CLAUDE.md

## Overview

Voice dictation for Linux/Sway/Wayland. Default model is parakeet-tdt_ctc-110m,
run on a discrete GPU when there is one, and chosen because it is the smallest
thing here that is still good without one. The menu runs from a 33 MiB
moonshine to a 1.8 GiB audio-LLM; parakeet-tdt-0.6b-v2 is the best of them on
both the leaderboard's close-microphone sets and the clip measured here, and is
what the README recommends to anyone with a card. Go binaries built via nix `buildGoModule`.
Models are downloaded on demand into the user's cache, not at build time.
Speech recognition is transcribe.cpp throughout, linked in through its Go
bindings; there is no second engine.

It is kept for breadth rather than for any one model. The alternatives were
weighed once the measurements landed: sherpa-onnx runs the parakeets well but
reaches a GPU only through CUDA, which means an unfree closure, and drops
every audio-LLM in the menu; parakeet.cpp is ggml and Vulkan like this one and
brings streaming with end-of-utterance detection, but does parakeet and
nothing else. Parakeet wins today and the field turns over every few months,
so the engine that takes a new family as another GGUF is worth more than the
engine that is fastest at the current favourite.

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
  startup is queued rather than killing the process. Recording is unlimited:
  the utterance ends when the user ends it, and anything past what the model or
  the card can take is cut into pieces (see Audio length below). Separately, a sample count in
  `internal/audio` bounds the buffer against a device that lies about its frame
  rate, which an ALSA null device once did to the tune of 19279s of audio in 4s
  of wall clock.
- `model.go` - lists the menu, or switches to an entry by number, by name or
  by path (via SIGHUP), fetching it first if the cache lacks it. There is no
  separate download verb: naming a model is the only reason to want one, and
  the prompt keeps the fetch from being silent.
- `cmd/transcribe/` - runs the daemon's pipeline over WAV files, for
  comparing models on fixed audio. Warms first, like the daemon, since an
  unwarmed model cuts long audio differently and that changes the transcript.
  `-limit` overrides where it cuts, which is how the effect was measured.
  The flake's `subPackages` builds only `cmd/diktat`, so this is never
  installed: it is `go run ./cmd/transcribe` in the devShell. Go rather than a
  script because it has to run the real pipeline, which a script would have to
  reimplement.
- `internal/models` - the menu. Thirteen entries, none bundled: everything is
  downloaded into `~/.cache/diktat/models` from the `handy-computer` GGUF
  repos, so no model is a special case. Downloads are never implicit. The
  menu carries a language set per model, hand-kept because the menu has to
  answer before a model is downloaded, and checked against the library by a
  test for whatever is present. Entries are ordered by download size, which is also roughly the cost order, and the
  number in that listing is how models get selected. Whisper encodes a padded
  30 second window whatever was said, so on a 2 second utterance it costs the
  same as on a 30 second one, while every other family here encodes only what
  it was given; the whispers earn their place on languages and on being the only
  family whose memory does not grow with the audio. Several entries are
  recent enough to have no published accuracy figure and are there to be
  tried, not because they are known good.
- `internal/asr` - one `Model` over transcribe.cpp. There is no backend
  interface any more: every model is a GGUF and the library reads the
  architecture out of it, so moonshine and whisper are not distinguishable
  here. Picks the discrete GPU when there is one.
- `internal/wav` - WAV read/write, split out from `internal/audio` so the
  offline tools can read a clip without pulling in malgo.
- `internal/warmup` - the rehearsal: the synthesised speech, the loop over the
  buckets, and what each one cost. Shared by the daemon and the offline tool
  because warming is not only about latency; it is also what tells a model how
  much audio it can take in one graph.
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

External CLIs expected on PATH: `wtype`, `wl-copy`, `wl-paste`, `swaymsg`,
`espeak-ng` (the warmup rehearses on synthesised speech).

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

## Warmup

Loading a model is not the same as being ready to use it: the Vulkan backend
compiles its shaders on the first graph run, and ggml allocates its compute
buffers there too. Both are per graph shape, and the shape follows the length
of the audio, so this is not a cost that is paid once.

After a load the daemon transcribes throwaway speech at each length bucket in
`internal/audio`, 1, 2, 3, 5, 7, 10, 15, 20, 25 and 30 seconds. The backend
picks its matmul variants in bands rather than per sample, so rehearsing inside
a band warms the whole band, and the buckets only have to be dense enough to
enter every band once. Measured from a cold shader cache, they leave arbitrary
lengths from 0.3s to 29.6s compiling nothing.

Which lengths earn a bucket is readable rather than guessed. transcribe.cpp
reports the names of the kernels a device has compiled, not just how many, and
`cmd/warmbench` prints what each length built that no shorter one had. The
names carry the variant, so the bands come out directly: on moonshine, 2s
brings in the q8_0 medium tile, 5s the f16 large tile, 15s the q8_0 large tile
and 20s its aligned form, and no other bucket builds anything. On canary the
productive lengths are 1, 2, 3, 7 and 25 instead, which is why the set is the
union rather than any one model's.

Sparse buckets do not work and the holes are not where the architecture
suggests. Rehearsing at 1 and 30 seconds left moonshine compiling a shader at
20 seconds and granite at 2, 5 and 10, three to four seconds each. The GGUF
says which families pad to a fixed window, but ggml-vulkan picks its variants
from the matrix dimensions and the device's core count, so which shapes are
distinct is a property of the pair, not of the model.

Two things were tried and reverted, both of them ways to make the coverage
exact rather than dense:

- Rounding every utterance up to a bucket. The encoder work it adds is charged
  forever, where the compiles it avoids are paid once: a 3.2s utterance rounded
  up to 5s cost granite 264ms against 80ms, and parakeet 46ms against 23ms.
  Only very short audio is still lifted, to the smallest bucket, because below
  that the shape is genuinely unrehearsed: canary spent 2.4s on 0.4s of audio
  and now spends 12ms.
- Cutting long audio into bucket-sized pieces. The models window long audio
  themselves and do it better: cutting a 60 second clip at 30 gave "was
  henceforth to be the victim." followed by "of a strange mystery." on every
  family, including ones that declare no limit at all. `audio.Chunk` now cuts
  only what a model would refuse or the card could not hold (see Audio length
  below).

Warming a model is not the whole of being warm. A card left alone drops its
clocks, so the first utterance after a pause pays to bring them back: on
granite, 25 seconds of quiet turned a 993ms encode into what costs 27ms back
to back. The daemon spends the time someone is speaking on a throwaway one
second run, started when recording starts, which absorbs all of it and hides
behind the speech. A model is single-threaded, so anything that touches one
waits for that run first.

Past the last bucket nothing is rehearsed. A dictation that long pays one
compile the first time it meets a shape, and the driver's on-disk cache keeps
it.

This is the same problem serving stacks meet with dynamic shapes, and the same
trade: vLLM captures CUDA graphs at a fixed set of batch sizes and pads to the
next one, and cuDNN's autotuner re-benchmarks per input shape, which is why
variable-length workloads are told to bucket. Their padding is cheap because a
padded batch slot is idle work; ours is not, because a longer clip is more
encoder work, which is why our buckets are dense but the padding is not.

The NVIDIA driver keeps compiled shaders in `~/.cache/nvidia/GLCache`, so a
warm run costs a few hundred milliseconds in the normal case, and only the
first one after a driver update pays the compile. `cmd/warmbench` measures all
of this: it rehearses one strategy per process and reports what each probe
compiled.

## Audio length

Every family here except whisper encodes the whole clip in one graph, and the
activations grow with its length, so there is a length at which the card cannot
hold the graph. The Vulkan backend does not survive that: the allocation fails
and the process dies with it, which for a daemon means dictation stops
mid-sentence and the bar goes dark. Whisper is exempt because it windows to 30
seconds internally, so its cost is flat at any length.

`MaxAudio`, which is the model's own declared ceiling, does not predict this
and cannot. Measured on the 8 GB laptop card, granite advertises 6m24s and dies
at 3 minutes; canary-180m-flash advertises no limit at all and dies at about 5;
Qwen3-ASR-1.7B dies at a little over a minute. A 151 MiB canary holds 4.3 GiB
after one four minute clip.

So the limit is measured rather than declared. ggml keeps its compute buffers
at the high-water mark, so a clip no longer than one already run allocates
nothing and is always safe; `Model.Transcribe` reads the device across the
clips that are longer, which are the only ones that allocate, and the drop is
attributable to that model rather than to everything loaded since.
`Model.AudioLimit` is then the longest clip already run plus what a quarter of
the device's free memory buys at that measured rate, capped by `MaxAudio` if
the model declares one. A quarter because this extrapolates past every length
measured and the slope taken from short clips understates the true one by up to
half again across the four families checked; being wrong the safe way costs a
seam, and the other way costs the daemon. The estimate re-anchors on every clip
that runs, so the limit climbs with use.

A model that has run nothing has no rate, and gets 30 seconds: what the warmup
rehearses to and what whisper windows to, so every model takes it, and running
it supplies the measurement. Anything that warms first never meets this floor,
which is why `internal/warmup` is shared rather than living in the daemon: a
tool that skips warming cuts audio the daemon would not, and where the cut
falls changes what comes back. On `bench.wav` a 30 second cut cost
canary-180m-flash 19 points of word error rate and saved parakeet 4, so a
benchmark run without warming is not measuring the daemon's pipeline.

## Model cache

Every model loaded stays resident, so switching back is instant. That needs a
ceiling now the models are large: the laptop GPU has 8 GB shared with the
desktop, and ggml's context and compute buffers cost more than the weights do
for a small model, so nvidia-smi shows ~1.4 GB resident for a 44 MB file.

`asr.Load` measures the weights and context by reading the device's free memory
either side of the load; the compute buffers are added as transcriptions grow
them, by the same accounting as above, so a model's cost is current rather than
what it was at load. A backend reporting no memory falls back to the file size.
`overBudget` in `daemon.go` picks what to drop, oldest first, and never the
model in use, so too small a budget degrades to keeping exactly one model
rather than to keeping none.

The default budget is two thirds of what the device had **free** at startup,
not of its total. The remaining third has to cover the compute buffers of
whichever model is in use, and those grow with the length of the dictation;
struck against the total it would also be spending the memory the compositor
already holds, which on this laptop is 1.4 GB of the 8.

## IPC files

Split by what they hold rather than by who writes them. In `/tmp`, which is
world-readable, are the files that say what diktat is doing:

- `diktat-daemon.pid` - daemon PID
- `diktat-status` - Pango markup status string, read by the bar
- `diktat-model` - model file currently loaded
- `diktat-daemon.log` - log; records lengths and timings, never the text

In `$XDG_RUNTIME_DIR/diktat/`, which is per-user and mode 0700, is the file
that holds what was actually said:

- `last` - last transcribed text

An unset `XDG_RUNTIME_DIR` is an error rather than a fallback to `/tmp`: the
only fallback available is the place that file exists to stay out of, and
a Wayland session always sets it, since the compositor's socket lives there.

## Build

```
nix build
./result/bin/diktat model parakeet-tdt_ctc-110m
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
  default `parakeet-tdt_ctc-110m`. `diktat model` records its choice in
  $XDG_STATE_HOME/diktat/model instead of writing here, since this file is
  hand-authored; that choice outranks this key, and deleting it restores
  this one.
- `model_cache_mb` - ceiling on what resident models hold together. 0 takes
  two thirds of what the compute device had free at startup.
- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription

Unknown keys are reported rather than ignored. TOML drops them silently,
which is how a key that had quietly stopped meaning anything sat in a real
config looking like it worked.
