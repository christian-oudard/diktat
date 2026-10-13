# Debug mode

Setting `DIKTAT_DEBUG` in the daemon's environment logs how each step went as
well as what happened: audio levels, per-stage timings, the warmup's per-length
breakdown, and a load's split into waking, reading and opening. For each
dictation it logs the wait from the press that stops the recording to the text
being typed.

It also keeps every dictation in `$XDG_CACHE_HOME/diktat/debug/` (by default
`~/.cache/diktat/debug/`), a directory readable only by its owner:

- its audio, as Opus named for the time of the stop press;
- a line in `dictations.jsonl` with the audio's file name, the model, the
  length in seconds, the typed text, and each stretch the dictation was
  transcribed in: its start and end in seconds, how long it took, and its text.
  The last stretch is what followed the last pause. A dictation that was typed
  also has `wait_ms`: the total wait after the stop press, and its parts,
  which are finishing what the model was running when the key was pressed (a
  phrase, or the run that wakes the card), transcribing the last stretch, and
  typing.

A capture with no samples at all keeps nothing. Nothing deletes these files,
so delete the directory when you are done with them. `go run ./cmd/transcribe
--live` replays a kept recording through the same path the daemon used and
reports the wait.

Without `DIKTAT_DEBUG`, no audio or text is written for debugging.
