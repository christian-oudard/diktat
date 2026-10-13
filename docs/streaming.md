# Streaming

When the model in use streams (the menu's `streaming` feature), the daemon
hands it the recording as it arrives, a quarter second at a time, while the
key is still down. Pressing the key again feeds whatever is left, ends the
stream, and types the model's final transcript. The wait after the key is
the time to read that last moment of audio, not the whole dictation.

Nothing is typed while recording: the text appears on release, exactly as for
a model that does not stream. A streaming model is never cut at pauses, so the
phrase rule in `docs/phrases.md` applies only to models that do not stream.

The stream starts once the model is free after the key is pressed, so audio
from the first moments is fed in one piece rather than lost, and a dictation
over before that is fed whole when the key is pressed. A model switched during
the recording, or a stream that fails, leaves the dictation to be transcribed
whole, as a model that does not stream would.

`go run ./cmd/transcribe --stream` replays a recording through a streaming
model the same way and reports the wait after the audio ends. `diktat record
--text` streams too, and prints the words as the model commits them; see
`record.md`.


## Not done

Typing words as the model commits them, while the key is still down. The
committed text never takes anything back, so this never needs to erase, but
it can commit a word the model would later have corrected, and that word is
then in the document. Measured per word, a word is committed on average 0.8
seconds after it is spoken on nemotron-speech-streaming-en-0.6b, and 1.6 on
parakeet-unified-en-0.6b. One family, moonshine-streaming, commits only part of
its final transcript, so committed text cannot stand in for it.
