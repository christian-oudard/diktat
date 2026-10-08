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
