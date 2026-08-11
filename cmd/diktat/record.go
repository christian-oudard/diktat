// Record captures from the default mic (the same path the daemon uses),
// showing a live 8-segment level meter, and writes a WAV. Press Ctrl-C to
// stop. Useful for checking a mic actually produces signal before recording a
// real take.
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/christian-oudard/diktat/internal/audio"
	"github.com/christian-oudard/diktat/internal/wav"
)

func runRecord(args []string) {
	out := "recording.wav"
	if len(args) > 0 {
		out = args[0]
	}

	rec, err := audio.NewRecorder()
	if err != nil {
		log.Fatalf("recorder: %v", err)
	}
	defer rec.Close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)

	rec.Start()
	fmt.Fprintf(os.Stderr, "Recording to %s. Press Ctrl-C to stop.\n", out)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

loop:
	for {
		select {
		case <-sig:
			break loop
		case <-ticker.C:
			level := rec.Level()
			fmt.Fprintf(os.Stderr, "\r  %s  %.3f  ", meter(level), level)
		}
	}

	samples := rec.Stop()
	fmt.Fprintln(os.Stderr)
	if err := wav.WriteWAV(out, samples, audio.SampleRate); err != nil {
		log.Fatalf("write wav: %v", err)
	}
	fmt.Fprintf(os.Stderr, "Saved %s (%.1fs)\n", out, float64(len(samples))/float64(audio.SampleRate))
}

// meter renders the level as 8 segments on a perceptual (sqrt) scale, so quiet
// input still moves the bars.
func meter(level float64) string {
	const n = 8
	filled := int(math.Sqrt(level)*n + 0.5)
	if filled > n {
		filled = n
	}
	return strings.Repeat("█", filled) + strings.Repeat("·", n-filled)
}
