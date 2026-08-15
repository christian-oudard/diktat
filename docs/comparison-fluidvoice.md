# FluidVoice: what it is and what it knows

FluidVoice (altic.dev/fluid, `altic-dev/FluidVoice`) is the closest thing to a
competitor diktat has: local-first voice dictation, a global hotkey, text typed
into whatever has focus, a menu of downloadable speech models. It is macOS
where this is Sway, SwiftUI where this is a daemon and a status file, and
open-core where this is not.

Read at `d62adc9`, 2026-08-14.

Nothing here is a proposal. It is what the code does, what that costs them,
and which four of their ideas are worth taking.


## The two projects, by the numbers

|  | diktat | FluidVoice |
|---|---|---|
| language | Go | Swift / SwiftUI |
| source | 39 files, 3.9k lines | 154 files, 89.0k lines |
| tests | 845 lines | 6.7k lines, integration-heavy |
| history | 50 commits since 2026-08-03 | 986 commits since 2025-09-21 |
| pace | one author | 414 commits in 90 days, 10+ contributors |
| licence | — | GPLv3 (Apache-2.0 before 2026-02-23) |
| platform | Linux / Wayland / Sway | macOS 15+, Apple Silicon and Intel |
| distribution | `nix build` | Homebrew cask, signed release, auto-updater |

The size difference is mostly interface. `Sources/Fluid/UI` and
`Sources/Fluid/Views` are 29.4k lines between them, and `ContentView.swift`
another 4.4k: roughly 38% of the tree is screens. `SettingsStore.swift` is
5,697 lines on its own, and `ASRService.swift` 5,584. Their pipeline — capture,
model, transcribe, type — is perhaps 12k lines against our 3.9k, which is a
real but much smaller multiple than the headline suggests.


## They are running the same engine

This is the finding that matters and it was not expected.

`Package.swift` pins `altic-dev/transcribe-cpp-swift` at exactly 0.1.2:
Swift bindings to the same handy-computer transcribe.cpp that `internal/asr`
links through its Go bindings. `WhisperProvider.swift` builds its download URL
as `huggingface.co/handy-computer/<name>-gguf/resolve/main/<name>-Q8_0.gguf`,
which is the same organisation `internal/models/download.go` names in `hfOrg`,
and the same quantisation. On the whisper path we and they are fetching
byte-identical files and feeding them to the same C++.

The difference is what that path is *for*. For us transcribe.cpp is the whole
engine and every one of the thirteen menu entries goes through it. For them it
is the compatibility lane: whisper only, and whisper is what Intel Macs and
exotic languages get. Their fast path is `altic-dev/FluidAudio` — their own
fork, pinned to a branch — running CoreML on the Apple Neural Engine, and that
is where parakeet, nemotron and cohere live.

So the engine bet the CLAUDE.md describes ("the engine that takes a new family
as another GGUF is worth more than the engine that is fastest at the current
favourite") is one they made too, and then hedged. They pay for the hedge with
seven provider implementations behind one `TranscriptionProvider` protocol —
`FluidAudioProvider`, `NemotronProvider`, `ParakeetRealtimeProvider`,
`WhisperProvider`, `AppleSpeechProvider`, `AppleSpeechAnalyzerProvider`,
`ExternalCoreMLTranscriptionProvider` — each with its own cache directory,
download logic, readiness rules and failure modes. `internal/asr` deleted its
backend interface for exactly this reason and is one `Model` type.

Their menu is 16 entries against our 13, one of them disabled behind
`qwenPreviewEnabled = false`. Ours is broader per byte of code because it is
thirteen rows of a struct.


## What they have that we do not

**Live preview while speaking.** The headline feature, and the one worth
reading closely — see the section below.

**Post-processing.** Raw transcript into an LLM, out comes formatted text with
tone matched to the target app. `LLMClient.swift` talks to OpenAI, Groq or a
custom endpoint; "Fluid Intelligence" is a local model that does the same
without the network. This is a genuine capability gap and it is the thing their
marketing leads with, not the ASR.

**Command and rewrite modes.** `CommandModeService` turns an utterance into an
action — launch an app, run a Shortcut — via function calling.
`RewriteModeService` takes selected text and rewrites it in place. Both are
dictation reused as an interface rather than as a keyboard.

**A vocabulary layer with feedback.** `ParakeetVocabularyStore` does word
boosting; `AutomaticDictionaryCorrectionTracker` (814 lines) watches what the
user retypes after a transcription and proposes dictionary entries from it. We
have `vocabulary_hints` for whisper and nothing that learns.

**A localhost HTTP API.** `LocalAPIServer` binds a loopback-only `NWListener`
and routes `/v1/health`, `/v1/history`, `/v1/dictionary/*`, `/v1/transcribe`
and `/v1/postprocess`. Other programs on the machine can drive the dictation
stack. Our equivalent surface is SIGUSR1 and a file in `$XDG_RUNTIME_DIR`,
which is smaller and does less.

**Audio device competence.** `DirectCoreAudioInput` is 1,504 lines and there is
a whole cluster around it: `MicrophonePreferenceCoordinator`,
`AudioCaptureReadinessGate`, `AudioEngineRetirementDrain`, and a
`SilentPCMRecoveryWatchdog` that notices three consecutive windows of RMS below
1e-5 and rebuilds the capture graph. The last commit before the read was
"fix-bluetooth-mic-stabilization". This is scar tissue from real hardware and
we have very little of it — `internal/audio`'s sample-count bound exists
because an ALSA null device lied once.

**File and meeting transcription.** `MeetingTranscriptionService` transcribes
whole recordings in 20-minute chunks with speaker diarisation.
`cmd/transcribe` reads a WAV and prints text.


## What we have that they do not

**A warmed model.** They have no equivalent to `internal/warmup`. Searching
the tree for warming finds audio-engine prewarming — keeping the mic graph
alive so capture starts fast — and a single `hasCompletedFirstTranscription`
flag whose only job is to clear a spinner. The first real utterance after a
model load pays whatever the backend charges to compile and allocate for that
shape, and on Metal that is not free. `cmd/warmbench` measured this problem
for us and the buckets in `internal/audio` answer it. They have not met it,
which on the ANE may mean CoreML's ahead-of-time `.mlmodelc` compile absorbs
most of it — but their whisper path is Metal ggml, the same code we warm, and
it is unwarmed.

**Bounded audio.** `Model.AudioLimit` re-anchors on every clip that runs and
cuts only what the card could not hold, because on the 8 GB laptop card granite
dies at 3 minutes against a declared 6m24s. FluidVoice's live buffer is a
`ThreadSafeAudioBuffer` with no ceiling at all, and the final transcription
hands the whole thing to the provider. Their guard is a memory check at *load*
time in `WhisperProvider.prepare` comparing free pages against a static
`requiredMemoryGB` per model — which is the declared figure that we measured to
be wrong. A long enough dictation on the wrong model is the failure mode
`Model.Transcribe` was written to prevent.

**A resident-model budget.** `overBudget` in `cmd/diktat/daemon.go` keeps
several models loaded against two thirds of the device's free memory at
startup, oldest evicted first, never the one in use. Their providers unload on
switch and reload on switch back.

**GPU placement that is chosen.** `placement` in `internal/asr` walks the
device list for a discrete GPU and skips the integrated one, because on a
hybrid laptop the library would otherwise take the Intel chip.
`WhisperProvider` picks Metal on Apple Silicon and CPU on Intel — which is the
whole decision on a Mac, so this is a difference in problem, not in care.

**Reproducibility.** `nix build` against pinned inputs, versus an Xcode project
plus five SPM dependencies, two of them pinned to a moving branch.

**No telemetry.** `AnalyticsService` posts to PostHog (`eu.i.posthog.com`)
behind a consent gate, with an anonymous install ID; events are usage counts,
onboarding steps and model downloads, not text. It is bounded and honestly
built. It is also a thing we do not have and a promise we do not have to make.


## Live preview, measured against docs/streaming.md

`docs/streaming.md` set out three postures and recommended B — preview
somewhere that is not the target window — then C behind a config key.
FluidVoice built B, and it is instructive that they did not build it the way
that document assumed.

`ASRService.runStreamingLoop` is a timer, not a stream. Every
`streamingPreviewIntervalSeconds` — 0.6s by default — it copies the *entire*
buffer from sample zero and calls `transcribeStreaming` on all of it. The
result is diffed against the last one by `smartDiffUpdate`, which finds the
longest common word prefix and keeps it if the overlap exceeds half, and the
text is rendered into an overlay near the notch. Nothing is typed until the
user releases the key.

That is B, and it works, and it costs what re-encoding a growing buffer every
0.6 seconds costs. A thirty-second dictation is about fifty transcriptions
averaging fifteen seconds of audio each. The code knows: `isProcessingChunk`
drops a tick if the previous one is still running, `skipNextChunk` drops the
next one if a transcription overran its interval, and `supportsStreaming`
hard-codes `qwen3Asr` and the three largest whispers to `false` because they
cannot keep up at all. Three separate backpressure mechanisms for one feature.

Only `ParakeetRealtimeProvider` does it properly, and only because FluidAudio
hands it a `StreamingEouAsrManager`: `consumeDelta` drops the samples already
fed and appends the rest, so the work is linear in the audio and the model
carries its own state. That path also gets end-of-utterance detection. It is
one model of sixteen.

Two things follow for us. The first is that transcribe.cpp's `StreamText` —
`Committed` append-only, `Tentative` volatile, `AgreementN` as the dial — is a
better primitive than what FluidVoice is working with on fifteen of its
sixteen models, and `docs/streaming.md` was right that posture B is cheap. The
second is that the naive version is tempting, ships, and then needs three
layers of backpressure and a hand-maintained list of models too slow to use it.
If B gets built here it should be built on `StreamFeed` from the start.

Their open question 1 — does `Committed` grow smoothly on a 2–4 second
utterance — is not answered by their code, because they never had the API.


## The open-core line

The README says FluidVoice is GPLv3 and that Fluid Intelligence is "a separate,
privately maintained local AI runtime". The tree shows exactly where the line
falls. `PrivateAIProvider.swift` defines the full protocol surface —
`PrivateAIProviderFeatureProviding`, model registry, artifact download with
SHA-256, prefix KV-cache priming — and then supplies
`UnavailablePrivateAIProviderFeature`, a stub with `isAvailable = false`,
`modelIDs() -> []` and `model(id:) -> nil`. Twenty-odd call sites across the UI
and services branch on `PrivateFeatures.privateAIProvider`, so an OSS build
compiles, runs, and shows none of it.

So: the dictation app is genuinely GPLv3 and genuinely complete. The local
enhancement model that the front page leads with is not in it, and building
from source gets you the app without the feature the marketing is about. That
is a defensible arrangement and it is stated plainly in the README, which is
more than most open-core projects manage. It is also the reason a fork cannot
compete with them on their own headline.

The licence changed from Apache-2.0 to GPLv3 in February 2026, which is the
move of a project that expects to be copied.


## Worth taking

Ranked by value over cost, and all four are small.

1. **A localhost API.** Their `/v1/transcribe` and `/v1/history` cost about 950
   lines including four controllers. A loopback socket that accepts a WAV and
   returns text would make the daemon scriptable by anything on the machine,
   and the model is already loaded and warm — which is the entire expensive
   part. The natural shape here is a Unix socket in `$XDG_RUNTIME_DIR/diktat/`
   next to `last`, not a TCP port, and the mode-0700 directory already answers
   the access question their loopback check has to answer in code.

2. **Learning the dictionary from corrections.** `vocabulary_hints` is
   hand-authored and therefore mostly empty. Watching what the user retypes
   immediately after a transcription and offering the diff as a hint is a
   feedback loop we have no version of. Their tracker is 814 lines because it
   has an overlay and a training session; the useful core is much smaller.

3. **The silent-PCM watchdog.** `AudioCaptureIdlePolicy` counts consecutive
   windows below an RMS and peak floor and rebuilds capture. We already learned
   that an audio device can lie; this is the other half of that lesson, cheap
   to port, and PipeWire is not better behaved than CoreAudio.

4. **Download integrity.** `WhisperProvider` checks the finished file against
   `expectedDownloadBytes` and deletes it if it disagrees, retries three times
   with exponential backoff, and refuses a size that does not match
   `expectedContentLength`. `internal/models/download.go` should at minimum
   check length; a truncated GGUF is a confusing failure much later.

Not worth taking: the timer-driven whole-buffer preview, for the reasons above;
the seven-provider abstraction, which is a tax on their portfolio and not on
ours; and per-app tone profiles, which need the post-processing model first and
are a different product.

The one to think about rather than copy is post-processing. It is their real
differentiator, it is the reason a user picks them over a thinner tool, and
`canary-qwen-2.5b` is already in our menu as the one architecture here that
decodes with a language model. That is a different way to reach some of the
same ground, without a second model in the loop.
