"""Tests for the mock OpenAI Realtime layer.

These pin down what we expect transcript cleanup to do. The real
Realtime API + a small post-processor will need to satisfy roughly the
same contract for the rest of the system to behave.
"""

from voice_interface.voice import VoiceLayer, cleanup_transcript


class TestCleanupTranscript:
    def test_strips_filler_words(self):
        assert cleanup_transcript("uh hello world") == "Hello world."

    def test_strips_you_know_phrase_only(self):
        # "you" alone is fine; "you know" together is filler.
        assert cleanup_transcript("hey you know what") == "Hey what."
        assert cleanup_transcript("did you call her") == "Did you call her."

    def test_handles_multiple_fillers(self):
        out = cleanup_transcript("um, like, tell Lola I'll be ten minutes late or so")
        assert out == "Tell Lola I'll be ten minutes late."

    def test_capitalizes_and_punctuates(self):
        assert cleanup_transcript("hello") == "Hello."

    def test_preserves_existing_punctuation(self):
        assert cleanup_transcript("hello?") == "Hello?"

    def test_empty_input(self):
        assert cleanup_transcript("") == ""
        assert cleanup_transcript("   ") == ""


class TestVoiceLayerQueue:
    def test_user_says_then_listen(self):
        v = VoiceLayer()
        v.user_says("uh hi")
        cleaned, raw = v.listen()
        assert cleaned == "Hi."
        assert raw == "uh hi"

    def test_listen_when_empty(self):
        v = VoiceLayer()
        assert v.listen() is None

    def test_records_spoken_text(self):
        v = VoiceLayer()
        v.speak("Done.")
        v.speak("Anything else?")
        assert v.spoken == ["Done.", "Anything else?"]

    def test_queue_user_turns(self):
        v = VoiceLayer()
        v.queue_user_turns(["a", "b", "c"])
        assert [v.listen()[1] for _ in range(3)] == ["a", "b", "c"]
        assert v.listen() is None
