package main

// streamer transcribes a recording while it is being made, through a model
// that streams. The audio heard since the last feed is queued and fed in one
// go, off the caller's loop, so the recording never waits on the model. T is
// what the caller wants back: the words committed so far for record --text,
// the final transcript for dictation.
type streamer[T any] struct {
	// feed hands the model more audio and returns what it has made of
	// everything so far and whether that grew; finalize ends the stream and
	// returns the last of it. Both are nil until the model is ready.
	feed     func([]int16) (T, bool, error)
	finalize func() (T, error)
	queued   []int16
	running  *feedRun[T]
	failed   bool
}

// feedRun is one feed in flight. The goroutine fills in the result before
// closing done.
type feedRun[T any] struct {
	done    chan struct{}
	out     T
	changed bool
	err     error
}

func (s *streamer[T]) ready(feed func([]int16) (T, bool, error), finalize func() (T, error)) {
	s.feed, s.finalize = feed, finalize
}

func (s *streamer[T]) add(samples []int16) { s.queued = append(s.queued, samples...) }

// busy is closed when the feed in flight finishes, or nil when there is none.
// The model is the feed's until then.
func (s *streamer[T]) busy() <-chan struct{} {
	if s.running == nil {
		return nil
	}
	return s.running.done
}

// poll returns what the feed that finished since the last poll made, when it
// changed, and starts feeding what has queued meanwhile. After a failure it
// does nothing: one that would repeat on every feed is not worth repeating.
func (s *streamer[T]) poll() (T, bool, error) {
	var zero T
	if s.failed || s.feed == nil {
		return zero, false, nil
	}
	var r feedRun[T]
	if s.running != nil {
		select {
		case <-s.running.done:
			r = *s.running
			s.running = nil
			if r.err != nil {
				s.failed = true
				return zero, false, r.err
			}
		default:
			return zero, false, nil
		}
	}
	if len(s.queued) > 0 {
		s.start()
	}
	return r.out, r.changed, nil
}

// finish waits for the feed in flight, feeds what is left, and ends the
// stream, for when the recording has stopped.
func (s *streamer[T]) finish() (T, error) {
	var zero T
	if s.feed == nil {
		return zero, nil
	}
	if s.running != nil {
		<-s.running.done
		err := s.running.err
		s.running = nil
		if err != nil {
			s.failed = true
			return zero, err
		}
	}
	if s.failed {
		return zero, nil
	}
	if len(s.queued) > 0 {
		if _, _, err := s.feed(s.queued); err != nil {
			return zero, err
		}
		s.queued = nil
	}
	return s.finalize()
}

func (s *streamer[T]) start() {
	r := &feedRun[T]{done: make(chan struct{})}
	s.running = r
	samples, feed := s.queued, s.feed
	s.queued = nil
	go func() {
		defer close(r.done)
		r.out, r.changed, r.err = feed(samples)
	}()
}
