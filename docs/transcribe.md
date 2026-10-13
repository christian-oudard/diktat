# Transcribing a recording

`diktat transcribe <recording>` writes `<recording>_transcript.md`, a document
of who said what, and `<recording>_transcript.json`, the turns it was rendered
from. Giving it that `.json` instead of a recording renders the document again
without transcribing anything.

## Live output

The text is printed to stdout as it is transcribed, one line per turn with the
speaker and the time it starts:

    S01  0:00:03  Good morning, everyone.
    S02  0:00:07  Morning.

The log goes to stderr, so `2>/dev/null` leaves only the text.

## Resuming

A run saves its finished work as it goes, in `<recording>_transcript.partial.json`
beside the output: who spoke when, and the text of each piece transcribed so
far. Stopping a run, by Ctrl-C or anything else, loses at most the piece in
progress. Running the same command again picks up after the last finished
piece, and prints the text already transcribed before continuing.

The saved work is used only by a run over the same file, unchanged since, with
the same pipeline and the same options. Anything else starts over and replaces
it. Saved text is kept only if the run cuts the recording into the same pieces,
which a newer diktat may not; otherwise those pieces are transcribed again.
It is deleted once the transcript is written.

A pipeline that transcribes and attributes speakers in one pass over the whole
recording has no pieces, so it prints its text when that pass ends and has
nothing to resume from.
