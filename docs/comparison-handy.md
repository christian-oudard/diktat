# Handy: the upstream

Handy (`cjpais/Handy`, handy.computer, MIT) is not a competitor in the sense
FluidVoice is. It is where diktat's engine and diktat's models both come from.
`handy-computer/transcribe.cpp` is CJ Pais's, and so is the `handy-computer`
Hugging Face org that `internal/models/download.go` names in `hfOrg`. Diktat is
a small Go daemon built on Handy's foundation and pointed at Handy's model
shelf.

That makes this a different kind of read. FluidVoice was worth checking for
what a competitor knows. Handy is worth checking because it is the reference
implementation of the same engine, on the same platform, and every place the
two designs disagree is a place one of us has thought about something the other
has not.

Read at `0e50367`, 2026-08-19, v0.9.5.


## The two projects

|  | diktat | Handy |
|---|---|---|
| shape | daemon + CLI | Tauri desktop app |
| language | Go | Rust + React/TypeScript |
| source | 39 files, 3.9k lines | 25.9k lines Rust, 14.6k lines TS/TSX |
| tests | 845 lines | ~1.6k lines Rust (download, gguf, keys), 1 Playwright spec |
| history | 50 commits since 2026-08-03 | 818 commits since 2025-02-03 |
| people | one | 158 contributors, 149 commits in 90 days |
| licence | — | MIT |
| platform | Linux / Wayland / Sway | macOS, Windows, Linux (X11 and Wayland) |
| packaging | `nix build` | releases, plus a flake with NixOS and home-manager modules |
| menu | 13 curated entries | 69, generated |

The Rust half is where the comparison lives; the TypeScript is a settings UI we
have no equivalent of and do not want one.


## Where we agree, without having coordinated

The overlap is larger than the FluidVoice one and it goes deeper than the
engine.

- **transcribe.cpp for everything.** Handy's `catalog.json` is 69 entries and
  every one is `EngineType::TranscribeCpp`. The `transcribe-rs` ONNX dependency
  is still in `Cargo.toml`, but the comment there is explicit that it is the
  legacy path — Parakeet, Moonshine, SenseVoice, GigaAM, Canary, Cohere — and
  the catalog has already moved all of those onto GGUF. This is worth stating
  plainly because FluidVoice made the opposite call: transcribe.cpp for whisper
  only, CoreML for the fast models. The author of the engine did not hedge, and
  is finishing the migration in the direction diktat started from.
- **Vulkan, not CUDA.** `Cargo.toml`'s Linux target pulls transcribe-cpp with
  `features = ["dynamic-backends", "vulkan"]`, for the same reasons the
  CLAUDE.md gives: no unfree toolchain, covers three vendors. Windows x86_64
  gets the same; Windows-on-ARM is CPU-only because Adreno's Vulkan drivers are
  not there yet.
- **`wtype`.** Their Linux notes recommend `wtype` on Wayland, `xdotool` on
  X11, `dotool` for both. Same tool, arrived at independently.
- **Toggle from the command line.** `handy --toggle-transcription` routed to
  the running instance through Tauri's single-instance plugin is `diktat
  toggle` sending SIGUSR1. Same design, different transport.
- **Nix.** They ship `flake.nix` for x86_64 and aarch64 Linux plus
  `nix/module.nix` and `nix/hm-module.nix`. On this machine both projects
  install the same way.

One divergence in the same area: they compile transcribe-cpp with
`dynamic-backends`, so the ggml CPU variants and Vulkan are loadable modules
scored by ISA at runtime. We link them in statically, which the CLAUDE.md's
runtime contract records. Theirs is right for a binary shipped to unknown
hardware; ours is right for a binary Nix built for this one.


## What Handy has that we do not

**Streaming, shipped.** `managers/transcription.rs` runs a stream worker over
`session.stream(&run_options, &StreamOptions::default())` — `CommitPolicy::Auto`
— feeding PCM and emitting on `update.committed_changed || update.tentative_changed`.
`stream.text().committed` and `.tentative` go to the frontend as a
`StreamTextEvent`, and `RecordingOverlay.tsx` renders them in two spans with
different styling. Nothing is typed until `finalize_stream`, whose
`stream.text().full` is what gets pasted.

That is posture B out of `docs/streaming.md`, built the way that document
recommended, on the API that document identified. See below.

**A generated catalog.** `scripts/gen_catalog.py` walks the `handy-computer`
org and merges three sources: the model card's `transcribe_cpp` block for
capabilities and benchmarks, a range-read of the GGUF header for display
labels, and a hand-written `CURATION` table for the recommended set and the UI
copy. The output is committed and `include_str!`'d, so the binary ships a
complete 69-model list with zero network access. Speed and accuracy become
scores by two published constants — `100·(1 − e^(−rtf/8))` and
`100·e^(−wer/15)` — so the menu can rank models it has never run.

This is the direct answer to something the CLAUDE.md names as a known
compromise: our `Langs` is "hand-kept because the menu has to answer before a
model is downloaded". Handy answers before download too, and does not hand-keep
it.

**Capabilities read from the file.** `managers/model_capabilities.rs` parses
`general.languages`, `stt.capability.streaming`, `stt.capability.translate` and
`stt.capability.lang_detect` straight out of the GGUF header, behind a
`CapabilityProber` trait with a `KNOWN_ARCHES` list of twenty-one
architectures. The catalog is described in its own doc comment as "a baked
probe" — the same shape with confident values. So a user-supplied GGUF that
nobody curated still gets an honest capability listing.

**Downloads that survive.** `managers/model/download` has 579 lines of tests
against local socket servers, resumable HTTP, per-file SHA-256, revision pinned
to a commit sha so `resolve/<sha>` is immutable, and a mirror list
(`blob.handy.computer`) tried after Hugging Face — untrusted, because every
byte is hash-checked anyway. `internal/models/download.go` checks nothing.

**A device picker.** `resolve_gpu_device` matches a persisted device key
against `transcribe_cpp::devices()`, and `is_transcribe_gpu_device` accepts
`Gpu` *and* `Igpu`. Where `placement` in `internal/asr` decides for the user by
skipping the integrated GPU, Handy puts every device in the settings UI with
its VRAM and lets them choose. Both answers are defensible; theirs needs a UI
and ours needs to be right.

**VAD.** Silero behind a `VoiceActivityDetector` trait, with prefill, onset and
hangover frame counts tuned separately for offline and streaming
(15/2/15 against 15/2/55). We record whatever the user recorded.

**Post-processing, custom words, history.** `llm_client.rs` is 734 lines of
OpenAI-compatible client with per-provider quirks for suppressing reasoning
tokens. `apply_custom_words` does fuzzy correction against a user list
(`strsim`), and `remove_filler_words` is gated on a language detected from the
text itself (`whatlang` bridged to the model's ISO 639-1 codes through
`isolang`). History is SQLite with migrations.

**Clipboard paste done carefully.** `paste_tx/` publishes the transcript as a
lazy clipboard promise and waits for the OS to report that something actually
*read* it before restoring the old clipboard — `WM_RENDERFORMAT` on Windows,
`pasteboard:provideDataForType:` on macOS — with a quiet period for apps that
read twice and an eight-second cap. It is careful work and it is only needed
because they paste. On Linux they fall back to the fixed-delay restore, which
is the bug the module exists to fix. Diktat types through `wtype` and never
touches the clipboard, so this whole category does not exist for us.


## What we have that Handy does not

Three things, and they are the same three that FluidVoice was missing. That is
now two independent implementations of this problem that do not do them, which
is either evidence the problems are ours alone or evidence they are unowned.

**A warmed model.** Searching the tree for warming finds `preload_vad` and a
background thread that pre-enumerates compute devices so the settings pane does
not freeze. There is no rehearsal of the model at any audio length. The first
utterance after a load pays whatever ggml-vulkan charges to compile and
allocate for that shape — on a cold shader cache, the seconds `cmd/warmbench`
measured. They are running the same backend on the same API, so this is not a
difference in platform.

**A bound on how long a dictation can be.** No cap anywhere: no maximum sample
count, no chunking in the batch path, no free-memory check. They read
`memory_total` off each device but only to print VRAM in the picker. The
failure `Model.AudioLimit` was written against — granite advertising 6m24s and
dying at 3 minutes on an 8 GB card — is available to them unmodified. Streaming
mitigates it for the streaming models, since the graph shape stops growing, but
the batch path is most of the catalog.

**A resident-model budget.** `lock_engine` holds one `Option<LoadedEngine>`
behind a mutex, and `ModelUnloadTimeout` defaults to unloading it after five
idle minutes. Switching models means loading from scratch, and so does dictating
after lunch. Diktat's `overBudget` keeps several models resident against two
thirds of the device's free memory and evicts oldest-first, never the one in
use. Theirs is the right default for a desktop app that must not look like it is
hoarding RAM; ours is the right default for a daemon that exists to be instant.

Also ours, though less load-bearing: no clipboard round-trip, no telemetry
question to answer, and a build with no `node_modules` in it.


## docs/streaming.md, answered

That document set out three postures, recommended B — preview somewhere that is
not the target window, type the final text on release — then C behind a config
key, and closed with three open questions. Handy has shipped B on the exact API
the document identified, so two of the three are now answered by someone else's
code.

*"Does `Committed` grow smoothly enough on a 2–4 second utterance to be worth
watching, or does it arrive in one lump at the end?"* — Smoothly enough to
ship. `RecordingOverlay.tsx` styles committed and tentative differently and
scrolls the pair as they grow; nobody builds that for text that arrives at the
end. Their `StreamPerf` logs `revision`, `input_received_ms`,
`audio_committed_ms` and `buffered_ms` per feed, which is the instrumentation
the document proposed writing.

*"Does feeding audio during recording cost enough CPU to affect the capture?"*
— Not enough to stop them, and their answer to the risk is structural: the
stream runs on its own worker thread over an `mpsc::Receiver<StreamCmd>`, so
`Feed`, `Finalize` and `Cancel` are messages and the capture thread never
blocks on the encoder.

*"Is there a wrong-commit rate at which C is intolerable?"* — Still open, and
they have not tried to answer it: they never type committed text, only preview
it. Which is itself informative. The person who wrote the engine, with the
`AgreementN` dial in reach, shipped the posture that cannot put a wrong word in
someone's document.

Two details their implementation adds that the document did not anticipate. If
`session.stream()` fails or the model does not stream, they drain the channel
so the finalize handshake still completes and fall through to batch — the
fallback path is inside the same worker, not a branch at the call site. And
`finalize` reads `stream.snapshot().language` as the last piece of
language evidence, so streaming feeds the post-processing decisions too.


## Worth taking

Ranked by value over cost. The first two would change the menu; the rest are
small.

1. **Read the capabilities out of the GGUF.** `general.languages` and the
   `stt.capability.*` keys are in the file, and `internal/models`'s hand-kept
   `Langs` is a list of what those keys already say. The header is a few
   hundred bytes at a known offset and an HTTP range request reaches it without
   downloading the model, which removes the reason the list is hand-kept in the
   first place. `models_test.go` already checks the hand-kept set against the
   library for whatever is present; this makes the check the source.

2. **Verify what was downloaded.** Pin the revision to a commit sha so
   `resolve/<sha>` is immutable, check the length, check a SHA-256 if the repo
   publishes one. Handy has 579 lines of tests here because a truncated GGUF
   fails much later and confusingly. Ours has none, and `diktat model` is the
   one place a user waits on the network.

3. **Streaming preview, on `StreamFeed`.** `docs/streaming.md` recommended it,
   Handy shipped it, and the two open questions that gated it are now answered
   in the affirmative by shipped code. The status file already carries Pango
   markup to the bar, which is the surface B needs; the escaping the document
   flagged is the only new work in the output path. Keep the worker-thread
   shape: a channel between capture and encoder, and a drain-to-batch fallback
   for the models that do not stream.

4. **VAD on the recording, if only for the log.** Silero behind an interface,
   with a hangover tail, would let the daemon skip transcribing a toggle that
   caught no speech — which today costs a full encode to discover.

Not worth taking: the clipboard paste transaction, which is careful work in
service of a design we do not share; the desktop settings UI; and the ONNX
second engine, which they are in the process of removing anyway.

The thing to notice rather than copy is the catalog generator. Sixty-nine
entries stay honest because a script regenerates them from the org, and thirteen
stay honest because one person is watching. That holds at thirteen. It is the
reason the menu is thirteen.
