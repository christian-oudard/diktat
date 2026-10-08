package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/christian-oudard/diktat/internal/audio"
)

// runRecord captures from the microphone until Esc or Ctrl-C and writes the
// audio to the current directory as Opus, for `diktat transcribe` to take
// later. The capture is the daemon's own path, 16 kHz mono, so a meeting
// recorded here is what the models expect without resampling.
//
// The audio goes to the encoder as it arrives, so the file grows on disk and
// memory stays flat however long the meeting runs. Closing the terminal ends
// the recording the way Esc does; a harder kill loses only the last moments.
func runRecord(args []string) {
	if len(args) > 0 {
		log.Fatal("usage: diktat record")
	}
	out := time.Now().Format("recording-2006-01-02T15-04-05.opus")

	rec, err := audio.NewRecorder()
	if err != nil {
		log.Fatalf("recorder: %v", err)
	}
	defer rec.Close()

	enc, err := startEncoder(out)
	if err != nil {
		log.Fatalf("write %s: %v", out, err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	keys := make(chan struct{}, 1)
	restore, err := readStopKeys(keys)
	if err != nil {
		log.Fatalf("terminal: %v", err)
	}

	rec.Start()
	fmt.Fprintf(os.Stderr, "Recording to %s. Press Esc to stop.\n", out)
	tr := newTrace(os.Stderr)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var n int
	write := func(samples []int16) {
		if err := binary.Write(enc.in, binary.LittleEndian, samples); err != nil {
			restore()
			log.Fatalf("write %s: %v: %s", out, err, enc.stderr())
		}
		n += len(samples)
		tr.add(samples)
	}
loop:
	for {
		select {
		case <-sig:
			break loop
		case <-keys:
			break loop
		case <-ticker.C:
			write(rec.Drain())
		}
	}
	write(rec.Stop())
	restore()
	fmt.Fprintln(os.Stderr)

	if err := enc.finish(); err != nil {
		log.Fatalf("write %s: %v", out, err)
	}
	fmt.Fprintf(os.Stderr, "Saved %s (%s)\n", out,
		(time.Duration(n) * time.Second / audio.SampleRate).Round(time.Second))
}

// readStopKeys sets the terminal on stdin to deliver keys as they are pressed,
// unechoed, and sends on keys for each Esc or Ctrl-C. Ctrl-C arrives as a byte
// rather than a signal, so it reaches only diktat: as a signal it also reaches
// whatever started diktat in the same terminal, and `go run` exits 1 on it
// after diktat has saved and exited 0. restore puts the terminal back.
//
// Without a terminal on stdin nothing is read, and only signals stop it.
func readStopKeys(keys chan<- struct{}) (restore func(), err error) {
	fd := os.Stdin.Fd()
	var old syscall.Termios
	if err := termios(fd, syscall.TCGETS, &old); err == syscall.ENOTTY {
		return func() {}, nil
	} else if err != nil {
		return nil, err
	}
	raw := old
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	if err := termios(fd, syscall.TCSETS, &raw); err != nil {
		return nil, err
	}
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if isStop(buf[:n]) {
				select {
				case keys <- struct{}{}:
				default:
				}
			}
		}
	}()
	return func() { termios(fd, syscall.TCSETS, &old) }, nil
}

// isStop reports whether one read from the terminal is Esc or holds a Ctrl-C.
// Esc is a lone escape byte: arrows and function keys begin with one too, but
// arrive with the rest of their sequence in the same read.
func isStop(b []byte) bool {
	return string(b) == "\x1b" || bytes.IndexByte(b, 0x03) >= 0
}

func termios(fd uintptr, req uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req,
		uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}

// encoder is ffmpeg turning raw samples on its stdin into Opus at 32 kbit/s,
// about 14 MB an hour. Measured on eleven minutes of one speaker through
// parakeet-tdt-0.6b-v2, 24k and above changed the same 0.2% of words against
// the uncompressed audio and 12k twice that; speech and audio modes did not
// differ. 32k is the margin for what that did not cover: several speakers, a
// distant microphone, and the diarizers.
type encoder struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	errs bytes.Buffer
}

func startEncoder(path string) (*encoder, error) {
	e := &encoder{}
	e.cmd = exec.Command("ffmpeg", "-v", "error", "-n",
		"-f", "s16le", "-ar", strconv.Itoa(audio.SampleRate), "-ac", "1", "-i", "-",
		"-c:a", "libopus", "-b:a", "32k", "-application", "voip", path)
	e.cmd.Stderr = &e.errs
	// Its own process group, so Ctrl-C or a closed terminal reaches only
	// diktat, which then ends the file. Signalled with the rest of the group,
	// ffmpeg stops at once and the file is left empty or cut short.
	e.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, err := e.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	e.in = in
	if err := e.cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	return e, nil
}

func (e *encoder) stderr() string { return strings.TrimSpace(e.errs.String()) }

// finish closes ffmpeg's input, which is what makes it write the end of the
// file, and waits for it.
func (e *encoder) finish() error {
	if err := e.in.Close(); err != nil {
		return err
	}
	if err := e.cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, e.stderr())
	}
	return nil
}
