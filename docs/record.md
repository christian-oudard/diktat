# Recording

`diktat record` captures the microphone and saves `recording-<date>.opus` in
the current directory. Esc or Ctrl-C stops it and exits 0, as does SIGINT,
SIGTERM, or closing the terminal. Other keys, including arrows and other keys
that begin with an escape byte, are ignored and not echoed.

While it runs, it draws the input level as a row of blocks, one per 100 ms of
audio, that fills the terminal and wraps like text.

A block's height shows how hard somebody is speaking, measured against how they
have been speaking, so that the same voice draws the same height on any
microphone at any volume. Each block scores its level, the RMS of its 100 ms in
dBFS, plus 1.5 times its brightness: the energy left after the filter
`x[n] - 0.95 x[n-1]` relative to the block's whole energy, in dB. A voice pushed
harder gets brighter as well as louder, and the brightness keeps rising where a
microphone holds the level near clipping. The speaker's typical score is the
75th percentile of the blocks that drew in the last 30 seconds; it draws in the
middle of the height `▄`, and each of the eight heights `▁`–`█` is 5 points, so
a score 10 above typical draws `▆` and 20 above it full height. The first block
that draws sets the typical score.

Background noise draws blank. The noise floor is the quietest block level of
the last five seconds, since speech keeps dropping back to the room between
words while steady noise does not move. Any block within 6 dB of it is blank,
and any block above that draws at least `▁`, so a whisper shows, though it is
quieter than the scale. A recording starts in the room, so its first block
sets the floor, and a recording begun mid-word draws that word blank until the
first pause. Digital silence, which a device delivers while it starts, is blank and does not
count towards the floor.

A block in which any sample reached full scale, so the audio clipped, draws as
`╋`.

## Text while recording

With `--text` or `-t`, it transcribes while it records, with the dictation
model, the one `diktat model` chose, which has to be one that streams. On the
menu that is parakeet-unified-en-0.6b. Words appear about a second after
they are said, once the model has settled on them, and do not change after
that. After Esc, the rest is transcribed and printed before the file is
reported saved.

On a terminal, the text is drawn as rows of words with a level over every
column, taken from the audio that column stands for. A word's letters stand
for the word's own audio, stretched or squeezed to their width; the
punctuation and space between two words stand for the time between them, so
a pause draws blank where the text pauses, and words that run together draw
no gap. The model's word times can be out by a few hundred milliseconds, so
each word is first fitted to the sound around it: it takes in all of its sound
and none of the silence beside it. The levels are on the meter's scale, so
steady background noise draws blank and anything louder draws.

The row being written is at the bottom, with the meter blocks for the audio
after its last word below it, one per 100 ms, as the meter draws them. Each
new word is added to that row and replaces its blocks. When the next word
does not fit, the row is finished, and stays where it is as the next row
starts below it.

Not on a terminal, the words are printed as they are finished, one sentence
per line. The meter goes to stderr and the text to stdout, so `diktat record
--text > notes.txt` keeps the text and shows the meter unbroken.

Recording starts at once; text starts once the model has loaded. Esc before
then waits for it, so a short recording still gets its text; Ctrl-C while it
waits skips the text. With a dictation model that does not stream, or none
downloaded, `--text` refuses to start and says what to choose or fetch. If
transcribing fails, the text stops there and the recording carries on; the
error is printed after the file is saved. Speaker labels come afterwards:
`diktat transcribe` gives them from the saved file.
