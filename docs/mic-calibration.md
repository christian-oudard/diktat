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

The daemon logs each capture's level and result to `/tmp/diktat-daemon.log`
(truncated on each start):

```
$ tail -f /tmp/diktat-daemon.log
```

Each transcription logs the capture's duration, peak, RMS, applied gain, and
the decoded text:

```
Transcribing <dur>s (peak <p> rms <r> gain <g>x)...
Transcribed in <t>: "<text>"
```

An empty result with low RMS points at the mic. An empty result with healthy
RMS (above ~0.04) points at the model path.

## 3. Capture a fixed corpus

To iterate on preprocessing without re-recording, capture a reference WAV once.
`diktat-record` captures to `recording.wav` at 16 kHz mono (the daemon's
capture path) and shows a live level meter so you can confirm the mic produces
signal before reading. Press Ctrl-C when you finish:

```
$ nix build
$ ./result/bin/diktat-record recording.wav
```

Read these five phonetically balanced sentences:

1. The birch canoe slid on the smooth planks.
2. Glue the sheet to the dark blue background.
3. These days a chicken leg is a rare dish.
4. The juice of lemons makes fine punch.
5. A pod of whales sped past the quiet cove.

Pass a different name to record several takes for comparison. Recording is from
the default source, so choose the mic first with `wpctl set-default <id>`. A
flat meter means no signal is reaching the capture path, even if system meters
look fine.

Captured `.wav` files are gitignored so voice recordings are never committed.

## 4. Run the offline pipeline

`diktat-transcribe` runs the exact Moonshine pipeline on WAV files, so a change
to preprocessing can be measured against the same audio every time:

```
$ nix build
$ ./result/bin/diktat-transcribe recording.wav
$ ./result/bin/diktat-transcribe -raw recording.wav
```

`-raw` skips normalization, so the two runs show what normalization changes.
Each line prints duration, peak, RMS, gain, and the decoded text.

Read the numbers this way: if quiet-but-audible speech (RMS above the floor)
still transcribes empty after normalization, the target RMS or preprocessing
needs adjusting. If only loud speech ever works, the mic gain is the problem,
fix it at step 1.

## 5. Compare ASR engines

If healthy audio still transcribes badly, the model is the suspect, not the
capture. `scripts/compare-asr.sh` runs the same WAVs through moonshine at two
sizes and whisper at two sizes, so the comparison is on your voice and your mic
rather than on a published benchmark:

```
$ nix develop
$ ./scripts/compare-asr.sh recording.wav
```

The daemon saves every capture to `/tmp/diktat-last.wav` before normalization,
so a bad transcription can be fed straight in without re-recording it.

Extra models are cached under `~/.cache/diktat-asr-compare` and never enter the
nix build, so the runtime closure stays as it is.

To run the daemon on a different moonshine size, point it at a cached one; the
architecture is read from the model, so nothing else needs to change:

```
$ MOONSHINE_MODEL_DIR=~/.cache/diktat-asr-compare/moonshine-base \
      ./result/bin/diktat-daemon
```
