# Voice Typing

Voice dictation for Linux/Wayland, on transcribe.cpp with a GPU when there is
one.

## Install

    $ nix profile add .

Not alongside the home-manager module below. `~/.nix-profile/bin` comes first
on PATH, so a profile install shadows the managed one and never moves again:
`diktat version` goes on reporting the build from the day it was installed
while every rebuild updates a binary nothing runs. `nix profile remove diktat`
undoes it. Pick one or the other.

## Usage

Fetch a model, run `diktat daemon` for the whole session, and bind
`diktat toggle` to a key in your Sway config:

    $ diktat model parakeet-tdt_ctc-110m   # offers to download it

    bindsym XF86Favorites exec diktat toggle

- First press: starts recording
- Second press: stops recording, transcribes, types result

## Running the daemon

The daemon holds the model for the whole session, so it wants a service
manager rather than a line in a window manager config: something has to
restart it when it dies, cap what it holds, and keep a second copy from
starting. There is no single-instance guard, and the second daemon overwrites
the first one's PID file, so `toggle` reaches only the newer one while the
older keeps its model resident for the rest of the session.

Pick whichever of these matches the session. All of them end with the same
thing running; they differ only in what starts it.

### With nix, via home-manager

    imports = [ inputs.diktat.homeManagerModules.default ];

Installs the package and the unit together. The unit's `ExecStart` names a
store path, so an upgrade changes the unit, and home-manager restarts the
daemon on the new build during activation.

### Without nix, as a systemd user service

`~/.config/systemd/user/diktat.service`:

    [Unit]
    Description=diktat dictation daemon
    PartOf=graphical-session.target

    [Service]
    ExecStart=%h/.local/bin/diktat daemon
    Restart=on-failure
    RestartSec=2
    RestartPreventExitStatus=78

    [Install]
    WantedBy=graphical-session.target

Then `systemctl --user enable diktat`. Add `MemoryMax=` if the model in use
deserves a ceiling; what it needs is the model's, not the daemon's.

### Sessions a display manager starts

Nothing further. GNOME, KDE, and anything else launched by a display manager
reach `graphical-session.target` on X11 and on Wayland alike, which is what
the unit is wanted by, so the daemon starts with the session; `PartOf` stops
it when the session ends.

### Window managers started from a tty

sway under greetd, i3 under `startx` and the like never reach that target, so
say it in the window manager config, which then names what it starts:

    # sway
    exec_always systemctl --user start diktat.service
    exec swaymsg -t subscribe '["shutdown"]' && \
        systemctl --user stop diktat.service

    # i3
    exec_always --no-startup-id systemctl --user start diktat.service

`start` is idempotent, so a config reload is a no-op rather than a second
daemon, which is what `exec_always diktat daemon` would give.

Nothing here imports an environment. The daemon looks up the compositor's
sockets in `XDG_RUNTIME_DIR` whenever it types, so it does not need
`systemctl --user import-environment`, and restarting the compositor
mid-session does not strand it on a socket name nobody holds.

### No systemd at all

`diktat daemon` from whatever the session runs, `.xinitrc` or the window
manager's own autostart. Use the equivalent of `exec` rather than
`exec_always`, since a reload that runs it twice leaves two daemons behind.

### Checking on it

    $ diktat version           # the build, and whether the daemon matches it
    $ systemctl --user status diktat

The daemon logs to stderr, so under systemd the log is the journal:

    $ journalctl --user -u diktat -f

A config file that does not parse stops the daemon rather than starting it on
defaults, with the reason on the last line and exit status 78 (EX_CONFIG).
The unit does not restart on that status, so the message stays where it can be
read; fix the file and `systemctl --user start diktat`.

Note `--user`. This is a user unit, and a system-level `journalctl -u diktat`
matches nothing at all, which reads exactly like a daemon that never logged.

## How it works

One binary, one subcommand per job. `diktat` with no arguments lists them.

- `diktat daemon`: long-running process that keeps the model in RAM
- `diktat toggle`: sends SIGUSR1 to the daemon, which starts or stops recording
- `diktat repeat`: re-types the last transcription
- `diktat model`: lists models, or switches to one, fetching it if needed

The daemon loads the model once at startup and then sits idle, so the first
press records with no delay. It does not start recording on its own, and it
does not exit on its own.

## Which model

Two measurements, because neither alone is enough. **Close** is the Open ASR
Leaderboard's word error rate averaged over its four close-microphone English
sets, which is what dictation into a headset resembles; its own headline
average includes far-field meeting rooms and accented conference calls, which
you will never dictate in, and that reorders the table. **Here** is a
64-second recording made on this machine, written to be hard: project jargon,
homophones only context can settle, numbers spelled out.

**Resident** and **longest** are measured on one laptop RTX 4070 with 8 GB
shared with the desktop. Resident is what the model costs the card once warm,
which is several times the file. Longest is how much audio it takes in one
graph before the recording has to be cut in two, because for every family but
whisper the compute buffers grow with the length of what you said.

    model                     MiB   close   here   resident   longest
    moonshine-tiny             33    8.65  36.8%    148 MiB       49s
    whisper-base.en            60       -  25.7%    179 MiB    10m14s
    parakeet-tdt_ctc-110m      96    4.46  18.4%    266 MiB     6m53s
    canary-180m-flash         151    3.96  33.8%    733 MiB      2m5s
    parakeet-tdt-0.6b-v2      514    3.52  12.5%    764 MiB     4m21s
    parakeet-tdt-0.6b-v3      523    4.07  15.4%    773 MiB     4m17s
    whisper-large-v3-turbo    590    4.27      -          -         -
    Qwen3-ASR-0.6B            615    4.00  29.4%    1.9 GiB     1m14s
    canary-1b-flash           733    3.42      -          -         -
    Qwen3-ASR-1.7B           1447    3.49  23.5%    3.1 GiB      1m5s
    cohere-transcribe-03     1688    3.41  35.3%    2.3 GiB     1m38s
    granite-speech-4.1-2b    1699    3.82  19.1%    2.4 GiB     1m25s
    canary-qwen-2.5b         1891    3.34  23.5%    3.4 GiB     1m29s

**Use parakeet-tdt-0.6b-v2.** It is the best English model here on both
measurements, which is the only reason to trust either of them, and at 568ms
for a minute of audio it is among the fastest things in the menu. Take
parakeet-tdt-0.6b-v3 instead if you dictate in a European language: nine more
mebibytes buys twenty-four more of them, at half a point of English accuracy.

**Use parakeet-tdt_ctc-110m without a GPU, or where 96 MiB matters.** It gives
up two points and is the fastest thing here on a CPU that has to do the work,
223ms on a short utterance against 1115ms for the 0.6b. moonshine-tiny is the
floor below that, 33 MiB and 99ms on a CPU, for twice the error rate.

Nothing above 600 MiB earned its size for dictation. They are slower, they hold
gigabytes, and every one of them cuts a dictation short of two minutes.
`canary-qwen-2.5b` is the accuracy ceiling of anything that will run here and
it is worth trying if you want it; it costs 3.4 GB of an 8 GB card and 1.9
seconds an utterance.

**Do not read the "here" column as a ranking.** The passage is 139 words, so a
three-point difference is four words and well inside the noise; the two
columns disagree on canary-180m-flash by a wide margin, and `canary-qwen-2.5b`
moved nine points between runs purely because it stopped cutting the clip in
two. It is a sanity check on one speaker's voice, and the leaderboard column is
the one averaged over four datasets.

## Switching models

The daemon can swap models in place, so another model can be judged against
live dictation without restarting the session:

    $ diktat model                    # the menu, then pick one; Enter keeps
    $ diktat model 3                  # or go straight to the third entry
    $ diktat model parakeet-tdt_ctc-110m   # or name it

Switching to a model that is not in the cache offers to fetch it first, so
there is nothing to type twice. The menu numbers are the short way in; the
names are long and the list is short.

Every model is one GGUF file, run through transcribe.cpp linked in process.
Moonshine and whisper are the same kind of thing to the daemon: which
architecture a file holds is read out of the file, so nothing here special
cases a family.

No model ships with the build, so every one is downloaded into
`~/.cache/diktat/models` and they are all on the same footing. Downloads are
never implicit: starting the daemon without the model it wants tells you what
to type rather than fetching it. Anything with a slash is used as a path, so an
out-of-menu model still works.

Only the model in use is held: the one it replaces is freed as soon as the new
one can transcribe, so a switch down to a small model gives the card its memory
back. The old one keeps serving while the new one loads, and a swap does not
interrupt a recording in progress, since the capture buffer is independent of
the model.
If the new model fails to load, the daemon keeps serving with the one it has.

Whisper always encodes a padded 30-second window, so a 2-second utterance
costs it the same as a 30-second one, while the rest encode only what they
were given. On this laptop's CPU, the smallest whisper against
parakeet-tdt_ctc-110m: 1045ms vs 136ms on a 2-second utterance, 960ms vs
235ms on a 3-second one, then 2365ms vs 2335ms at 30 seconds and 2639ms vs
4768ms at 55. The flat cost is a liability up to about half a minute and an
asset past it, and dictation is mostly short utterances, so the menu leads
with the models that scale with the audio. On a GPU the gap narrows, since
the padded encoder parallelises well, and the reason to prefer parakeet
becomes accuracy per megabyte rather than latency.

The same window makes whisper the only family whose memory is flat: its
compute buffers cost the same on a second of audio as on five minutes, where
everything else grows with the length until the card cannot hold the graph.
That is why the menu keeps a small whisper, and it is the reason the "longest"
column above says 10m14s for whisper-base.en and 1m5s for a model six times
its size.

A switch does not persist across a daemon restart; the daemon always comes
back on the model named in the config, or on the default.

### Why a switch pauses for about a second

A GPU left alone powers down: clocks drop and the PCIe link falls back to Gen1,
which NVIDIA documents as normal (`nvidia-smi` reports link generation "may be
reduced when the GPU is not in use"). Loading a model is almost entirely
host-to-device transfer, so it is hit hard by this. Measured on a laptop RTX
4070, the same model loaded in 0.7 seconds on a card that had just been working
and in over two minutes on one left idle for half a minute.

So before loading, the daemon transcribes one second of throwaway synthesised
speech, which brings the card back up. It waits for that to finish rather than
waiting out any fixed delay. On a card already working it takes about 130ms; on
one that has gone to sleep it takes about 0.9 seconds, and that difference is
the hardware waking rather than anything diktat does. It is why a switch takes
a second or so rather than being instant. Dictation pays the same cost but you
never see it, because the throwaway run happens while you are still talking.

If you would rather not pay it, that is a machine-wide setting rather than a
diktat one. `sudo nvidia-smi -pm 1` plus `sudo nvidia-smi --lock-gpu-clocks=…`
(reset with `-rgc`), or on AMD `power_dpm_force_performance_level`, hold the
clocks up for everything on the machine and spend idle power to do it. On a
laptop that is a poor trade, which is why it is not the default.

The daemon's log reports both halves, so a machine that behaves differently
says so:

    Model now parakeet-tdt-0.6b-v2 … in 1.01s (woke 1.626s, read 151ms, open 857ms)

`woke` is what waking the card cost and `open` is the load itself. A long
`woke` means the card was asleep; a long `open` after a short `woke` is
something else.

To compare models on fixed audio instead of live, `go run ./cmd/transcribe
-model <name> file.wav` runs any of them over the same WAVs. It is a
development tool rather than part of the daemon, so it is not installed; run
it from a checkout. See `docs/mic-calibration.md`.

## Memory

The daemon is resident for the whole session, so its memory is bounded on
purpose:

- Freshly loaded, before any transcription: about 530 MB
- Steady state after a few minutes of use: about 1310 MB, flat

Those were measured with moonshine alone; the whispers are roughly half: 275
MB loaded, settling near 680 MB. The newer and larger
entries in the menu cost considerably more: canary-qwen-2.5b settles around
3.4 GB on the card, which is why only one model is kept.

Switching with `diktat model` frees the model being left as soon as the new
one is ready, so the daemon holds one model rather than every model tried. It
matters because a model costs far more than its file on disk, since ggml's
context and compute buffers outweigh the weights of a small model several
times over: the daemon measures the real cost at load and again as
transcriptions grow the buffers, and says both in the log.

Recording runs until you stop it. The sample buffer grows at 32 KB/s while it
does, and a model's compute buffers grow with the length of the audio, so a
very long dictation costs memory on the card rather than in the daemon: on an
8 GB laptop GPU the largest models cannot allocate a graph for much past half
a minute. A separate sample-count guard bounds the buffer against a capture
device that reports frames faster than real time.

Run it from a systemd user unit so it starts with the session and can be
restarted after an upgrade:

    $ systemctl --user restart diktat

Signals sent while the model is still loading are queued, so a keypress during
startup is not lost.

## Suspend

Suspending the machine discards the card's memory, model included, unless the
driver was told to preserve it, which stock NVIDIA settings do not. The daemon
notices the resume within a couple of seconds and reloads the model in the
background, so dictation works by the time you are back at the keyboard; a
dictation that catches the reload midway waits for it rather than typing
nothing.

The driver can be told to keep video memory across a sleep instead, which
makes the reload redundant (it still runs, and costs a few background
seconds). On NVIDIA that is the `NVreg_PreserveVideoMemoryAllocations=1`
module parameter, or on NixOS:

    hardware.nvidia.powerManagement.enable = true;

The cost is at every suspend: the driver writes everything live on the card
out to a temporary file under `NVreg_TemporaryFilePath` (default `/tmp`,
tmpfs recommended by NVIDIA), sized up to the whole of VRAM, so suspends get
slower and need that much space free. With the daemon reloading on its own,
this is a nicety rather than a requirement.

## Updating

The keybinding resolves `diktat` at each press, so a new build takes
effect on the next press. The daemon keeps the old code in RAM until restarted,
though. To check which build is live:

    $ diktat version

This prints the commit it was built from and when, and adds a line when the
running daemon is some other build. The daemon logs the same on startup.

## Stop the daemon

    $ systemctl --user stop diktat
