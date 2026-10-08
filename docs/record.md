# Recording

`diktat record` captures the microphone until Ctrl-C and saves
`recording-<date>.opus` in the current directory.

While it runs, it draws the input level as a row of blocks, one
per 100 ms of audio, that fills the terminal and wraps like text. Quiet speech
draws one or two eighths high, medium speech three to five, and loud speech six
to eight.

A block's height is its level, the RMS of its 100 ms in dBFS, drawn from -45,
blank, to -5, full height, in eight heights `▁`–`█`, 5 dB each. The range is
set from labelled takes into a laptop microphone and a headset, where it puts
quiet, medium and loud speech near those heights on both.

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
