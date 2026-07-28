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

Each transcription logs peak, RMS, applied gain, and the decoded text, for
example:

```
Transcribing 2.0s (peak 0.478 rms 0.0165 gain 3.6x)...
Transcribed in 20ms: ""
```

An empty result with low RMS points at the mic. An empty result with healthy
RMS (above ~0.04) points at the model path.

## 3. Capture a fixed corpus

To iterate on preprocessing without re-recording, capture reference WAVs once.
Read these five sentences (phonetically balanced) in one take per recording:

1. The birch canoe slid on the smooth planks.
2. Glue the sheet to the dark blue background.
3. These days a chicken leg is a rare dish.
4. The juice of lemons makes fine punch.
5. A pod of whales sped past the quiet cove.

Record three takes at whisper, normal, and loud volume. Use 16 kHz mono to
match the daemon's capture. Press Ctrl-C to stop each:

```
$ pw-record --rate 16000 --channels 1 --format s16 whisper.wav
$ pw-record --rate 16000 --channels 1 --format s16 normal.wav
$ pw-record --rate 16000 --channels 1 --format s16 loud.wav
```

Add `--target <id>` to capture from a specific source instead of the default,
so you can compare mics on identical speech.

Captured `.wav` files are gitignored so voice recordings are never committed.

## 4. Run the offline pipeline

`diktat-transcribe` runs the exact Moonshine pipeline on WAV files, so a change
to preprocessing can be measured against the same audio every time:

```
$ nix build
$ ./result/bin/diktat-transcribe whisper.wav normal.wav loud.wav
$ ./result/bin/diktat-transcribe -raw whisper.wav normal.wav loud.wav
```

`-raw` skips normalization, so the two runs show what normalization changes.
Each line prints duration, peak, RMS, gain, and the decoded text:

```
normal.wav       6.8s  peak 0.389  rms 0.0253  gain  2.4x  ->  "..."
```

Compare the takes: if quiet-but-audible speech (RMS above the floor) still
transcribes empty after normalization, the target RMS or preprocessing needs
adjusting. If only loud speech ever works, the mic gain is the problem, fix it
at step 1.
