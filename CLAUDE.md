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
- `daemon.go` - keeps the model loaded, rehearses it between dictations (see
  Warmup below), bounds the resident set
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
- `internal/warmup` - the rehearsal: the synthesised speech, which lengths to
  rehearse at, and one length at a time. A whole run of them is here too, for
  a caller with nothing else to do; the daemon drives the lengths itself,
  because it interleaves them with dictations and only it knows when one is
  happening. Shared with the offline tool
  because warming is not only about latency; it is also what tells a model how
  much audio it can take in one graph.
- `internal/xdg` - the two base directories, so that `config` and `ipc` do not
  each have their own idea of where state goes. A leaf: `ipc` asking `config`
  for a path put the model downloader, and so the http stack, behind every
  build of a package that names four files.
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

All four Wayland ones need `WAYLAND_DISPLAY` and `SWAYSOCK`, and the daemon
runs as a systemd user service wanted by `default.target`, so it starts before
any compositor and inherits neither. `internal/output/env.go` finds them in
`XDG_RUNTIME_DIR` when a child is spawned: the live `sway-ipc.<uid>.<pid>.sock`
gives SWAYSOCK, and `WAYLAND_DISPLAY` is read out of that compositor's
`/proc/<pid>/environ`, rather than guessed from the `wayland-N` names sitting
beside it, which a greeter or a nested session also leaves behind. An
inherited value always wins, which is the case for anything a keybinding runs.

The alternative was `systemctl --user import-environment` in the compositor
config, which is the documented way and has two costs this does not: it makes
the compositor responsible for starting the daemon, and it copies the values
once, so a compositor restart leaves the daemon typing at a socket nobody
holds, since SWAYSOCK names sway's PID. Looking up per child costs a glob and
a small read on a path that already spawns three processes.

Two live compositors for one user is an error rather than a pick. Either
socket is plausible and the wrong one puts a dictation on the wrong screen.

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

The daemon transcribes throwaway speech at each length bucket in
`internal/audio`, 1, 2, 3, 5, 7, 10, 15, 20, 25 and 30 seconds. The backend
picks its matmul variants in bands rather than per sample, so rehearsing inside
a band warms the whole band, and the buckets only have to be dense enough to
enter every band once. Measured from a cold shader cache, they leave arbitrary
lengths from 0.3s to 29.6s compiling nothing.

It runs between dictations rather than before the first one. A model can
transcribe as soon as it is loaded, and rehearsing it costs several seconds on
a large one, so the daemon installs it first and then works through the buckets
one at a time in the gaps: a bucket is scheduled only while nothing is
recording and no other model is loading. Starting a recording cancels the
bucket in flight, which the library honours between decode steps but not
inside the encoder, so a bucket lets go within tens of milliseconds on an
audio-LLM and at the end of its encoder on everything else. A cancelled length
keeps its place and comes round again, since the run may have stopped before
it compiled anything. Progress is kept per model, so switching to a model
rehearsed earlier resumes where it left off, and one that finished is not
rehearsed again.

What that costs is the early dictation. Until a model has run a bucket it has
no measured rate and cuts audio at the 30 second floor (see Audio length), and
a length no bucket has reached yet compiles when it is first met. Both are
paid by whoever dictates in the first seconds after a switch, where before
they were paid by everyone waiting for the switch to finish.

None of it is on the bar. LOAD is lit while a model is being read and goes out
when it can transcribe, which is what the bar is for: saying whether dictating
is possible right now. A rehearsal in the background is not that, so it lives
in the activity file, which is where `diktat model` reads it.

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
to back. The daemon spends the time someone is speaking on a one second run,
started when recording starts, which absorbs all of it and hides behind the
speech. A model is single-threaded, so anything that touches one waits for
that run first.

A suspend is how a card loses a model wholesale: unless the driver was told
to save video memory across a sleep, the weights are discarded, and a model
without them does not fail. It runs its graphs at the usual speed and returns
nothing, on every utterance, until something reloads it, so every dictation
after a resume typed nothing and the log said only "0 chars". The daemon
notices the sleep itself rather than inferring it from the silence:
CLOCK_BOOTTIME is CLOCK_MONOTONIC plus time spent suspended, so their
difference is a ledger of sleep that nothing else moves, NTP included, and
`internal/suspend` reads it on a two second ticker. When it grows, the model
is reloaded off the main loop, so the reload is under way before anyone is
back at the keyboard. Every load carries the suspend count it started under,
and one that was reading the card when the machine slept is closed unopened
and run again. This needs nothing from logind or D-Bus, and it counts sleeps
nothing orchestrated, `echo mem` included. On a machine whose driver does
preserve video memory the reload is redundant and costs a few background
seconds, which is the conservative side of that bet. A model on the CPU is
not reloaded at all, since RAM is exactly what a suspend preserves.

The wake run is the backstop behind that, since it is the only audio all
session whose words are known before it is transcribed. It catches what the
ticker cannot: a dictation begun inside the reload window, and video memory
lost to anything that is not a sleep. Silence from it where there were words
before means the model is gone, and the daemon reloads before transcribing
the capture it is holding, so the dictation that finds it is not lost; if the
ticker's reload is already in flight, it is waited out rather than raced with
a second copy of the same model, which a large one could not fit beside.

Only a model that has answered the clip before is judged, or one with no words
for it would be reloaded before every dictation. That baseline comes from the
rehearsal rather than from the first dictation, since the buckets run the same
speech within a second or two of every load: taken from the first dictation, a
machine suspended before anyone dictated would leave a model that had never
answered, so the silence after the resume would not be a change from anything
and the daemon would stay mute for the rest of the session.

Reloading may not be enough:
it replaces the model but not the device it lives on, and what was known to
work was restarting the process. So the reload is checked against the same
clip, and a reload that is still mute exits, which the unit's `Restart` turns
into that restart. This one load runs on the main loop rather than off it,
unlike every other load here, because a model that cannot transcribe leaves
nothing to answer a keypress with in the meantime.

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
it supplies the measurement. The daemon meets this floor now that it serves
before it rehearses, but only until the first bucket lands, which is a second
or so after the switch. A tool that skips warming altogether stays on it, and
where the cut falls changes what comes back: on `bench.wav` a 30 second cut
cost canary-180m-flash 19 points of word error rate and saved parakeet 4, so a
benchmark run without warming is not measuring the daemon's pipeline. That is
why `internal/warmup` is shared rather than living in the daemon.

## One model resident

The model in use is the only one held, and the one it replaces is closed as
soon as the new one can transcribe. Nothing is cached against a switch back.

It was a cache once, with an LRU and a budget, because a reload is seconds and
a switch back was instant without one. What that bought stopped being worth
its price when the menu grew: canary-qwen-2.5b holds 3.4 GiB, of which 1.5 GiB
is compute buffers, and a laptop card has 8 GB shared with the desktop.
Holding that for a model nobody is using is worse than reloading it, and
neither ggml nor the driver hands it back on its own, so the memory stays gone
until the daemon lets go of it. Reloading also costs less than it used to:
the load happens off the main loop and the rehearsal happens after it, so a
switch is a few seconds of the old model still serving.

That load runs off the main loop, so a keypress is still answered while it
happens, and the old model keeps serving until the new one can transcribe.
Only one load runs at a time: a second ask cancels the first rather than
queueing behind it, since the newer request is the one to be honoured and
waiting out a 2 GB load nobody wants any more is tens of seconds of nothing.
`asr.Load` itself cannot be interrupted, so a cancellation lands when it
returns and the model it produced is closed unopened.

What a model costs is still measured rather than guessed, since the log says
it and the audio limit is struck against it: `asr.Load` reads the device's
free memory either side of the load for the weights and context, and the
compute buffers are added as transcriptions grow them. A backend reporting no
memory falls back to the file size.

How long it took is split the same way. A load used to be one number, and a
523 MiB model that took 1m59s of it was blamed on the card being full, then on
the weights going over PCIe a tensor at a time, before anyone could say which
half of the load it was even in; both were wrong, and neither was cheap to
rule out. So `asr.Load` reads the file through once and discards it before
handing the path to the library, which reads it again from the page cache that
read just filled. The two numbers separate waiting for a disk from everything
the library does afterwards, which is the only seam visible from here:
`transcribe.Open` is one call and logs nothing timed. The extra read is close
to free on a warm cache, and on a cold one it is the same read either way,
only now it is the half that has a number on it.

## IPC files

Split by lifetime. In `$XDG_RUNTIME_DIR/diktat/`, which is per-user, mode
0700 and emptied by logind when the user's last session ends, are the files
that describe the session:

- `daemon.pid` - daemon PID, so `toggle` knows where to send its signal
- `model` - model directory currently loaded; a request on the way in and a
  statement of fact on the way out
- `last` - last transcribed text, which is what makes `repeat` possible
- `activity` - what the daemon is busy with, as `loading <dir>` or
  `warming <dir>`, and absent when it is busy with neither. `model` cannot
  answer this: it names the model in use, and the whole question during a
  switch is what is happening to the one that is not in use yet

An unset `XDG_RUNTIME_DIR` is an error rather than a fallback to `/tmp`: the
only fallback available is the place these exist to stay out of, and a
Wayland session always sets it, since the compositor's socket lives there.

In `$XDG_STATE_HOME/diktat/`, alongside the remembered model choice, is the
one file another program reads:

- `status` - Pango markup string saying what the daemon is doing, read by the
  bar: REC, TX, and LOAD while a model is being read

Both toggles write it before anything else the press sets off, and TX covers
everything between the press and the text: waiting out the wake run, reloading
a model that has gone quiet, and the transcription itself. What the daemon
happens to be doing when the key arrives is not the light's business, and REC
through any of that says the mic is live when it is not.

It is there rather than with the others because a bar's config has to name
the path, and `/run/user/<uid>` cannot be written as `~`; i3status resolves a
tilde, not a uid. The cost is that a daemon killed outright leaves its last
status on screen, where a runtime file would have gone at logout. The next
start overwrites it.

All three used to be `/tmp/diktat-*` under fixed names. Two users on one
machine could not both run a daemon, since the second one's write landed on a
file it did not own, and mode 0644 published what diktat was doing to
everything else running.

The log is not among these at all. It goes to stderr, which is the journal
when systemd runs the daemon and the terminal when a person does, and it
records lengths and timings, never the text.

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
- `paste_methods` - map of sway app_id to paste key combo (`C-v`, `C-S-v`)
- `history_file` - JSONL append target for each transcription. A path, or
  `true` for `$XDG_STATE_HOME/diktat/history.jsonl`, or `false` for no
  history, which is also what leaving the key out does. It holds what was
  said, so the file is 0600.

Unknown keys are reported rather than ignored. TOML drops them silently,
which is how a key that had quietly stopped meaning anything sat in a real
config looking like it worked.

A key with the right name and the wrong type is worse than an unknown one:
the decode fails, and every caller here answered that by carrying on with an
empty Config. `history_file = false` in a real config is how that was found,
months after the paste_methods table below it had stopped being read, and the
only report was a line in a log that was truncated at every start. Hence
`history_file` taking a bool at all, and hence the log going to the journal.
