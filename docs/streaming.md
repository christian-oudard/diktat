# Streaming: a proposal

Diktat records to the end of an utterance, transcribes it whole, and types the
result. Streaming models transcribe as you speak. This is what adopting one
would mean, and what it would cost.

Nothing here is built. It is written down so the decision is made once, in the
open, rather than discovered halfway through an implementation.


## What actually changes

Not the model loading, and not the recording. `internal/audio` already
delivers samples in callback-sized chunks; today they are appended to a buffer
and handed over at the end. Streaming feeds them to the model as they arrive.

What changes is **the output path**, and only because of one property:

> A streaming model may revise text it has already emitted.

The model hears "recognise speech", emits it, hears two more words, and
decides it was "wreck a nice beach" all along. Offline models do this too --
they just do it before you ever see the text.

Diktat types into whatever window has focus. Retracting typed text means
sending backspaces into someone else's editor, which is not safe: the cursor
may have moved, the target may have autocompleted, the keystrokes may land
somewhere else entirely. **Diktat must never need to un-type.**


## The library already solves the retraction half

transcribe.cpp does not hand you one moving transcript. `StreamText` returns
three views, and the middle one exists for exactly this problem
(`include/transcribe.h`):

- `Full` -- the raw current hypothesis. Authoritative, and any of it can be
  rewritten on the next feed.
- `Committed` -- *"its bytes are append-only for the life of the stream"*.
- `Tentative` -- the volatile suffix after the committed prefix.

`Committed` never shrinks and never rewrites. Type it as it grows and no
backspace is ever required. This was verified when the Go bindings were
written: `TestStreamCommittedGrows` asserts `strings.HasPrefix(committed,
previous)` after every feed and after finalize, and it holds against
moonshine-streaming-tiny.

So the retraction problem does not reach the input layer. What replaces it is
subtler, and the header is equally explicit:

> `committed_text` is independent from the raw hypothesis and **may not be a
> prefix of `full_text`** after the model revises already-committed text.

Committed text never retracts, but it can end up disagreeing with what the
model finally believes. You typed it. It stands. It may be wrong.

Which means the name oversells it. "Committed" guarantees irrevocability, not
agreement: the API will never take those bytes back, and the model may still
end up believing something else. Durable but not consistent. Read it as
"emitted, and you are stuck with it" rather than as "settled".

The revision problem does not disappear. It converts from *"retract text in
someone's editor"*, which is unacceptable, into *"occasionally commit a word
the model would later have corrected"*, which is a quality trade you can tune.
`StreamOptions.AgreementN` is the dial: how many consecutive hypotheses must
agree on a prefix before it commits. Default 3. Higher commits later and is
wrong less often.


## Three postures

**A. Commit on finalize.** `CommitPolicy: CommitOnFinalize`. Nothing commits
until the stream ends. Byte-identical output to today, so a streaming model
can be adopted with no change to the output path at all. Buys nothing except
the ability to *use* streaming models.

**B. Preview elsewhere, type on release.** Stream continuously, render
`Committed`+`Tentative` somewhere that is not the target window -- diktat
already publishes Pango markup to the status file for the bar -- and type the
final text on release exactly as now. Live feedback, zero commitment risk, the
input path untouched. The transcript you get is the offline-quality one.

**C. Type as it commits.** Type each new `Committed` byte as it appears. Words
appear while you speak. Never backspaces. Occasionally leaves a word the model
would have fixed, and that word is now in your document.

**Recommendation: B first, then C behind a config key.** B is a strict
improvement with no downside and no risk to the text; it also builds the
streaming plumbing that C needs. C is the interesting one, but it trades
accuracy for latency in someone's real editor, and that trade should be opt-in
and reversible.


## What B costs

- `internal/asr` grows a streaming path: `StreamBegin` / `StreamFeed` /
  `StreamFinalize` alongside `Transcribe`. The Go bindings already expose all
  of it.
- The recorder hands chunks to the model as well as to the buffer. The buffer
  stays: the whole capture is what gets normalized and transcribed at the end.
- The status file carries partial text as well as state. It is Pango markup,
  so the text needs escaping, which it does not need today.
- Model switching gets a rule it does not have: a stream is per-session state,
  so `reloadModel` mid-stream must finalize or reset first.

None of that touches `output.Type`.


## What the menu costs

Only some families stream, and today's menu is chosen for offline accuracy.
`Capabilities.SupportsStreaming` says which, and the ones worth having are a
different set:

| model | avg WER | size | note |
|---|---|---|---|
| moonshine-streaming-medium | 5.8% | 295 MB | better than whisper-large-v3-turbo, and streams |
| moonshine-streaming-small | 6.9% | 198 MB | |
| nemotron-speech-streaming-en-0.6b | 5.7% | 538 MB | |
| parakeet-unified-en-0.6b | — | 540 MB | cache-aware and buffered variants |

So the menu grows a streaming section, and `Spec` grows a `Streams bool` --
kept by hand like `Vocab` and `Langs`, checked against the library by the same
test.

A model that does not stream is not an error under posture A or B: it falls
back to the offline path, and the daemon says so. Under C it would have to,
since there is nothing to preview.


## What this does not solve

Vocabulary hints. Only whisper takes them, and no whisper model streams, so
posture C and `vocabulary_hints` are mutually exclusive today. Voxtral's
realtime variant streams and takes an instruction, but it is ~3 GB and scores
worse than parakeet.

Punctuation and capitalisation land late. Families that emit them do so once
enough context exists, so early committed text can be less polished than the
same words would have been offline. Under C that difference is visible in the
document.


## Open questions

1. Does `Committed` grow smoothly enough on a 2-4 second utterance to be worth
   watching, or does it arrive in one lump at the end? Untested. Measure
   before building C.
2. Does feeding audio during recording cost enough CPU to affect the capture?
   The encoder runs per feed rather than once.
3. Is there a wrong-commit rate at which C is intolerable, and what
   `AgreementN` reaches it?

(1) is cheap to answer and gates the rest: log `StreamUpdate.Revision` and
`Committed` length per feed for a few real utterances and look at the shape.
