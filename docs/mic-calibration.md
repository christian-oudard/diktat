# Microphone calibration

How to diagnose empty or missing transcriptions and check that your mic feeds
Moonshine a usable signal.

## Background

Moonshine returns empty text when the input is too quiet. Empirically the
threshold is around RMS 0.04. Normal speech should sit at RMS 0.05 or higher.
The daemon peak-corrects quiet input (see `internal/audio/process.go`), but a
mic that is muted, low-gain, or narrowband can still fall short. This procedure
finds where the signal is being lost.

## 1. Check the capture level

Confirm the default source is the mic you expect and that its volume is up:

```
$ wpctl get-volume @DEFAULT_AUDIO_SOURCE@
$ wpctl status | grep -A8 Sources
```

If the volume is low, raise it:

```
$ wpctl set-volume @DEFAULT_AUDIO_SOURCE@ 1.0
```

To make a different mic the default (for example the laptop's built-in mic
instead of a Bluetooth headset), use its id from the `Sources` list:

```
$ wpctl set-default 65
```

Note: a Bluetooth headset used as a mic switches to the HFP/HSP profile, which
is narrowband and applies its own gate and gain. A wired or built-in mic gives
cleaner, wider-band audio and usually transcribes better.

## 2. Read the daemon log

The daemon logs each capture's level and result to stderr, which is the
journal when systemd started it:

```
$ journalctl --user -u diktat -f
```

Each transcription logs the capture's duration, peak, RMS and applied gain,
then what the decode cost and how much text came back. The text itself is
never logged, so the length is what says whether anything was heard:

```
Transcribing <dur>s (wall <w>s, peak <p> rms <r> gain <g>x)...
Transcribed in <t> (mel <m>, encode <e>, decode <d>, other <o>): <n> chars
```

Zero chars with low RMS points at the mic. Zero chars with healthy RMS
(above ~0.04) points at the model path.

## 3. Capture a fixed corpus

To iterate on preprocessing without re-recording, capture a reference WAV
once. The daemon does not keep captures, so record one deliberately, at the
rate and shape the daemon asks its own device for:

```
$ pw-record --rate 16000 --channels 1 --format s16 recording.wav
```

Dictating the same sentences through the daemon afterwards gives the levels to
compare against: its log line reports `peak`, `rms`, and the gain that
normalization applied, which is what a level meter would have told you. A peak
near zero means no signal is reaching the capture path, even if system meters
look fine.

Read these five phonetically balanced sentences:

1. The birch canoe slid on the smooth planks.
2. Glue the sheet to the dark blue background.
3. These days a chicken leg is a rare dish.
4. The juice of lemons makes fine punch.
5. A pod of whales sped past the quiet cove.

Copy it under a different name to keep several takes for comparison. Capture
is from the default source, so choose the mic first with `wpctl set-default
<id>`.

Captured `.wav` files are gitignored so voice recordings are never committed.

## 4. Run the offline pipeline

`cmd/transcribe` runs the daemon's own pipeline over WAV files, so a change
to preprocessing can be measured against the same audio every time. It is not
installed, so run it from a checkout:

```
$ nix build
$ go run ./cmd/transcribe recording.wav
$ go run ./cmd/transcribe -raw recording.wav
```

`-raw` skips normalization, so the two runs show what normalization changes.
Each line prints duration, peak, RMS, gain, and the decoded text.

Read the numbers this way: if quiet-but-audible speech (RMS above the floor)
still transcribes empty after normalization, the target RMS or preprocessing
needs adjusting. If only loud speech ever works, the mic gain is the problem,
fix it at step 1.

## 5. Compare ASR engines

If healthy audio still transcribes badly, the model is the suspect, not the
capture. `cmd/transcribe -model` runs any model in the menu over the same WAVs, so
the comparison is on your voice and your mic rather than on a published
benchmark:

```
$ for m in $(diktat model --names); do
      echo "== $m"; go run ./cmd/transcribe -model $m recording.wav
  done
```

Captures are not kept: a recording of every dictation is only worth writing to
disk if something replays it, and nothing does. Record a reference clip with
`pw-record` when you want one to compare models against.

Models are cached under `~/.cache/diktat/models` and never enter the nix build,
so the runtime closure stays small. To switch the running daemon instead of
transcribing offline, use `diktat model <name>`.
