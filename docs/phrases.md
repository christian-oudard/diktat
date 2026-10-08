# Transcribing while you speak

While a dictation is recording, the daemon transcribes what has already been
said, one phrase at a time, so that when the recording stops only the last
phrase is left to transcribe. The wait after the key press is the time to
transcribe that last phrase, however long the dictation was.

A phrase ends at a pause: at least 0.8 s in which the voice activity detector
(fsmn-vad) hears no speech, coming at least 4 s after the start of the phrase.
The cut falls in the middle of the pause, so no word is split. The latest such
pause is used, so when transcription falls behind the speaker, the next phrase
is longer rather than the queue.

The phrases' texts are joined with a space, in order, and typed as one text
when the recording stops, exactly as a dictation transcribed whole would be.
Nothing is typed while recording.

Each phrase is transcribed without the others, so a model cannot use words
after a pause to decide words before it. A pause of 0.8 s inside a sentence
may leave a capital letter or a full stop where the sentence was split.

The detector is fetched with the model by `diktat model`. Without it, the
daemon transcribes each dictation whole when the recording stops, and says so
once at startup.
