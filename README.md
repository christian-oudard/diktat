# Voice Typing

Voice dictation for Linux/Wayland, on transcribe.cpp with a GPU when there is
one.

## Install

    $ nix profile add .

## Usage

Fetch a model, run `diktat daemon` for the whole session, and bind
`diktat toggle` to a key in your Sway config:

    $ diktat model whisper-tiny.en    # offers to download it

    bindsym XF86Favorites exec diktat toggle

- First press: starts recording
- Second press: stops recording, transcribes, types result

## How it works

One binary, one subcommand per job. `diktat` with no arguments lists them.

- `diktat daemon`: long-running process that keeps the model in RAM
- `diktat toggle`: sends SIGUSR1 to the daemon, which starts or stops recording
- `diktat repeat`: re-types the last transcription
- `diktat model`: lists models, or switches to one, fetching it if needed

The daemon loads the model once at startup and then sits idle, so the first
press records with no delay. It does not start recording on its own, and it
does not exit on its own.

## Which model

Measured here on one laptop RTX 4070, transcribing a 64-second recording of
connected speech written to be hard: project jargon, homophones only context
can settle, and numbers spelled out. Word error rate is scored after
normalizing case, punctuation and number spelling; latency is a warm run on a
3.2-second utterance, which is what dictation actually costs.

    model                    MiB    WER   GPU     CPU
    moonshine-tiny            33  36.8%   19ms    99ms
    whisper-tiny.en           42  36.8%   30ms   983ms
    whisper-base.en           60  25.7%   42ms
    parakeet-tdt_ctc-110m     96  18.4%   20ms   223ms
    canary-180m-flash        151  33.8%   21ms
    whisper-small.en         184  25.0%   56ms
    parakeet-tdt-0.6b-v3     523  15.4%   41ms   1115ms
    Qwen3-ASR-0.6B           615  29.4%   52ms
    Qwen3-ASR-1.7B          1447  24.3%  106ms
    cohere-transcribe-03    1688  35.3%   58ms
    granite-speech-4.1-2b   1699  19.1%  100ms

**Use parakeet-tdt-0.6b-v3.** It is the most accurate model in the menu on
this recording and among the fastest, 41ms on a short utterance, quicker than
whisper-base.en at two thirds of its error rate. At 523 MiB it leaves room for
a second model resident beside it.

**Use parakeet-tdt_ctc-110m without a GPU, or where 96 MiB matters.** It gives
up three points of accuracy and is the fastest thing here on a CPU that has to
do the work: 223ms on the same utterance, against 1115ms for the 0.6b and
983ms for whisper-tiny.en, which pays for a padded 30-second window whatever
you said. moonshine-tiny is the floor below that, 33 MiB and 99ms on a CPU,
for twice the error rate.

Everything above 600 MiB was dominated on both axes: slower than the 0.6b
parakeet and less accurate. Two of them, granite and Qwen3-ASR-1.7B, could not
allocate a graph for the whole minute of audio on an 8 GB card shared with a
desktop, and had to be scored on halves.

Read the numbers as a ranking, not as absolutes. One speaker, one recording,
English, and jargon drawn from this project, on a passage built to be
adversarial: the same models score far lower error rates on ordinary prose.

## Switching models

The daemon can swap models in place, so another model can be judged against
live dictation without restarting the session:

    $ diktat model                    # the menu, then pick one; Enter keeps
    $ diktat model 3                  # or go straight to the third entry
    $ diktat model whisper-tiny.en    # or name it

Switching to a model that is not in the cache offers to fetch it first, so
there is nothing to type twice. The menu numbers are the short way in; the
names are long and the list is short.

Every model is one GGUF file, run through transcribe.cpp linked in process.
Moonshine and whisper are the same kind of thing to the daemon: which
architecture a file holds is read out of the file, so nothing here special
cases a family.

No model ships with the build, so every one is downloaded into
`~/.cache/diktat/models` and they are all on the same footing. Downloads are
never implicit: starting the daemon without the model it wants tells you what
to type rather than fetching it. Anything with a slash is used as a path, so an
out-of-menu model still works.

Every model stays resident once loaded, so switching back to one already seen
is instant, up to a memory ceiling: past it the least recently used ones are
dropped, never the one in use. Set `model_cache_mb` to change the ceiling; it
defaults to two thirds of the GPU's memory. A swap does not interrupt a
recording in progress, since the capture buffer is independent of the model.
If the new model fails to load, the daemon keeps serving with the one it has.

Whisper always encodes a padded 30-second window, so a 2-second utterance
costs it the same as a 30-second one, while the rest encode only what they
were given. On this laptop's CPU, whisper-tiny.en against
parakeet-tdt_ctc-110m: 1045ms vs 136ms on a 2-second utterance, 960ms vs
235ms on a 3-second one, then 2365ms vs 2335ms at 30 seconds and 2639ms vs
4768ms at 55. The flat cost is a liability up to about half a minute and an
asset past it, and dictation is mostly short utterances, so the menu leads
with the models that scale with the audio. On a GPU the gap narrows, since
the padded encoder parallelises well, and the reason to prefer parakeet
becomes accuracy per megabyte rather than latency.

A switch does not persist across a daemon restart; the daemon always comes
back on the model named in the config, or on the default.

To compare models on fixed audio instead of live, `go run ./cmd/transcribe
-model <name> file.wav` runs any of them over the same WAVs. It is a
development tool rather than part of the daemon, so it is not installed; run
it from a checkout. See `docs/mic-calibration.md`.

## Memory

The daemon is resident for the whole session, so its memory is bounded on
purpose:

- Freshly loaded, before any transcription: about 530 MB
- Steady state after a few minutes of use: about 1310 MB, flat

Those were measured with moonshine alone; whisper-tiny.en, the default,
is roughly half: 275 MB loaded, settling near 680 MB. The newer and larger
entries in the menu cost considerably more, which is what the cache budget
below is for.

Models switched into with `diktat model` stay resident, so switching back
costs nothing after the first load. That is bounded: past `model_cache_mb`
the least recently used are freed, never the one in use. The ceiling matters
because a model costs far more than its file on disk, since ggml's context
and compute buffers outweigh the weights of a small model several times
over; the daemon measures the real cost at load rather than guessing from
the file size.

Recording runs until you stop it. The sample buffer grows at 32 KB/s while it
does, and a model's compute buffers grow with the length of the audio, so a
very long dictation costs memory on the card rather than in the daemon: on an
8 GB laptop GPU the largest models cannot allocate a graph for much past half
a minute. A separate sample-count guard bounds the buffer against a capture
device that reports frames faster than real time.

Run it from a systemd user unit so it starts with the session and can be
restarted after an upgrade:

    $ systemctl --user restart diktat

Signals sent while the model is still loading are queued, so a keypress during
startup is not lost.

## Updating

The keybinding resolves `diktat` at each press, so a new build takes
effect on the next press. The daemon keeps the old code in RAM until restarted,
though. To check which build is live:

    $ diktat version

This prints the commit it was built from and when, and adds a line when the
running daemon is some other build. The daemon logs the same on startup.

## Stop the daemon

    $ systemctl --user stop diktat
