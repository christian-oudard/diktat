package main

import (
	"path/filepath"
	"testing"

	"github.com/christian-oudard/diktat/internal/sco"
)

// stubLinks points the link watch at a count a test owns rather than at the
// machine's bluetooth adapters.
func stubLinks(t *testing.T, n *int) {
	t.Helper()
	scoLinks = func() (int, error) { return *n, nil }
	t.Cleanup(func() { scoLinks = sco.Links })
}

// A headset switched off on purpose stays off, so the loss is answered once
// rather than at every tick for the rest of the session.
func TestLinkLossIsAnsweredOnce(t *testing.T) {
	links := 1
	stubLinks(t, &links)

	fake := &fakeRecorder{}
	d := &daemon{recorder: fake, cfg: emptyConfig(), linkWatch: true}
	d.checkLink()

	links = 0
	captureLog(t, func() {
		d.checkLink()
		d.checkLink()
		d.checkLink()
	})

	if fake.rebuilds != 1 {
		t.Errorf("rebuilt %d times for one lost link, want once", fake.rebuilds)
	}
}

// Zero links is also what a machine with no bluetooth at all reports, every
// two seconds, forever. Only losing a link that existed means anything.
func TestNoBluetoothIsNotALostLink(t *testing.T) {
	links := 0
	stubLinks(t, &links)

	fake := &fakeRecorder{}
	d := &daemon{recorder: fake, cfg: emptyConfig(), linkWatch: true}
	captureLog(t, func() {
		d.checkLink()
		d.checkLink()
	})

	if fake.rebuilds != 0 {
		t.Errorf("rebuilt %d times on a machine that has no bluetooth", fake.rebuilds)
	}
}

// A rebuild leaves a device with no link until the audio stack negotiates one,
// and the watch runs every two seconds. So the tick after a rebuild finds zero
// links on a device that is merely still coming up, and must not tear it down
// again: the second rebuild interrupts the negotiation the first one started,
// and the microphone stays dead through dictations that would have worked.
//
// The two repairs are what made this reachable. audio.IsDead rebuilds from the
// dictation path and the link watch rebuilds from the ticker, and the watch
// only knew about its own.
func TestARebuildDisarmsTheLinkWatch(t *testing.T) {
	dir := t.TempDir()
	statusPath = filepath.Join(dir, "status")
	activityPath = filepath.Join(dir, "activity")

	links := 0
	stubLinks(t, &links)

	fake := &fakeRecorder{samples: make([]int16, 16000)} // bit-exact zero
	d := &daemon{recorder: fake, cfg: emptyConfig(), linkWatch: true, linkSeen: true}

	// A dictation into a headset whose link has died.
	captureLog(t, func() { d.stopRecording() })
	if fake.rebuilds != 1 {
		t.Fatalf("the dead capture rebuilt %d times, want once", fake.rebuilds)
	}

	// The ticker, before the fresh device has a link.
	captureLog(t, func() {
		d.checkLink()
		d.checkLink()
	})
	if fake.rebuilds != 1 {
		t.Errorf("rebuilt %d times; the watch tore down a device that was still coming up", fake.rebuilds)
	}

	// The link comes up, which arms the watch again.
	links = 1
	d.checkLink()

	links = 0
	captureLog(t, func() { d.checkLink() })
	if fake.rebuilds != 2 {
		t.Errorf("rebuilt %d times; a link lost after the watch re-armed is still a loss", fake.rebuilds)
	}
}
