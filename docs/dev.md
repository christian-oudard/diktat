# Developer tools

`go run ./cmd/dev <command>`, inside the devShell. These measure and inspect
models; none of them is part of dictating, so they are not in the installed
`diktat`.

## replay

```
dev replay [--model <name>]... [--mode auto|whole|phrases|stream] [--last N] [recording...]
```

Replays recordings as if each had just been dictated, and reports the wait
from the end of the audio to the text, and the text.

The recordings are the files given, in any format ffmpeg reads. With none, they
are the recordings debug mode kept (`docs/debug.md`), oldest first, and
`--last N` takes the newest N of them.

`--model` may be given more than once. Each model is loaded in turn, warmed as
the daemon warms it, and given every recording. Without it, the model is the
one the daemon starts on.

`--mode` is how the audio reaches the model:

- `auto`, the default: whatever the daemon would do with that model. `stream`
  for a model that streams, else `phrases` when the voice activity detector is
  downloaded, else `whole`.
- `whole`: transcribed in one go once the audio ends.
- `phrases`: cut at pauses while it plays, as `docs/phrases.md` describes.
- `stream`: fed to the model every quarter second, as `docs/streaming.md`
  describes.

Nothing sleeps. A recording is replayed as fast as the model allows, and the
wait counts only what the model would still have had to do after the audio
ended, had the audio arrived at the speed it was spoken.

Each recording prints a line with its name and length. A recording debug mode
kept is followed by what was typed when it was dictated, and its wait. Then
each model prints one line: its name, the mode, the wait, and the text. A
stream also gives how long after it was spoken a word was committed, on
average and at most, and phrases give how many phrases were cut.

## caps

```
dev caps [model...]
```

What each model can do, read out of the GGUF: the devices on this machine and
their free memory, then per model the finest timestamps, the longest audio it
declares, and its features. With no arguments it reads every model in the
cache. It runs on the CPU, so it can run beside the daemon.

## diarize

```
dev diarize [--model <name>] [--preset <point>] [-o rows.tsv] recording.wav
```

Runs a diarizer alone over a recording and reports who spoke when: how many
speakers, how much of the recording each covers, and how much overlaps.
`--preset` is sortformer's operating point (`default`, `very-high`, `high`,
`low`); `-o` writes the speaker rows to a TSV.
