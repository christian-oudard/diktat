package transcribe

/*
#include <stdlib.h>
#include <transcribe/extensions.h>

extern bool transcribeAbortTrampoline(void * user_data);
*/
import "C"

import (
	"context"
	"unsafe"
)

// Role is a job a model can do. A model may serve more than one.
type Role uint32

const (
	// RoleASR models transcribe, through Session.
	RoleASR Role = C.TRANSCRIBE_ROLE_ASR
	// RoleDiarize models say who spoke when, through DiarizeSession, and
	// produce no text.
	RoleDiarize Role = C.TRANSCRIBE_ROLE_DIARIZE
)

// Roles are the jobs this model serves.
func (m *Model) Roles() Role { return Role(C.transcribe_model_roles(m.c)) }

// MaxSpeakers is the most speakers a diarizer tells apart, or 0 for one with
// no cap. ErrUnsupportedRole for a model that does not diarize.
func (m *Model) MaxSpeakers() (int, error) {
	var info C.struct_transcribe_diarize_info
	C.transcribe_diarize_info_init(&info)
	if err := check(C.transcribe_diarize_get_info(m.c, &info)); err != nil {
		return 0, err
	}
	return int(info.max_speakers), nil
}

// DiarizeExtension is a diarizer's own block of run options. The method is
// unexported, so the set is closed.
type DiarizeExtension interface {
	Kind() ExtKind
	// diarizeExt builds the typed struct in C memory, for the reason
	// RunExtension's runExt gives.
	diarizeExt() (*C.struct_transcribe_ext, func())
}

// DiarizeSession says who spoke when in a recording. Like Session, it is
// single-threaded.
type DiarizeSession struct {
	c *C.struct_transcribe_diarize_session
}

// NewDiarizeSession opens a diarization session on a model that serves
// RoleDiarize. threads is the CPU thread count, 0 for the library default.
// The model must outlive the session.
func (m *Model) NewDiarizeSession(threads int) (*DiarizeSession, error) {
	var p C.struct_transcribe_diarize_session_params
	C.transcribe_diarize_session_params_init(&p)
	p.n_threads = C.int32_t(threads)
	d := &DiarizeSession{}
	if err := check(C.transcribe_diarize_session_init(m.c, &p, &d.c)); err != nil {
		return nil, err
	}
	return d, nil
}

// Close frees the session. Closing twice is a no-op.
func (d *DiarizeSession) Close() {
	C.transcribe_diarize_session_free(d.c)
	d.c = nil
}

// Run diarizes one recording of 16 kHz mono samples. ext is nil for the
// model's defaults. Cancelling ctx stops the run with ErrAborted.
//
// Segments are grouped by speaker and time-ordered within one; segments of
// different speakers may overlap. P is NaN, since diarizers report no
// per-segment confidence.
func (d *DiarizeSession) Run(ctx context.Context, pcm []float32, ext DiarizeExtension) ([]SpeakerSegment, error) {
	if len(pcm) == 0 {
		return nil, ErrInvalidArg
	}
	var p C.struct_transcribe_diarize_params
	C.transcribe_diarize_params_init(&p)
	if ext != nil {
		e, free := ext.diarizeExt()
		defer free()
		p.family = e
	}
	defer watchAbort(ctx, func(cb C.transcribe_abort_callback, cell unsafe.Pointer) {
		C.transcribe_diarize_set_abort_callback(d.c, cb, cell)
	})()

	if err := check(C.transcribe_diarize_run(d.c, (*C.float)(&pcm[0]), C.int(len(pcm)), &p)); err != nil {
		return nil, err
	}

	out := make([]SpeakerSegment, int(C.transcribe_diarize_n_segments(d.c)))
	for i := range out {
		var c C.struct_transcribe_speaker_segment
		C.transcribe_speaker_segment_init(&c)
		if err := check(C.transcribe_diarize_get_segment(d.c, C.int(i), &c)); err != nil {
			return nil, err
		}
		out[i] = SpeakerSegment{Start: ms(c.t0_ms), End: ms(c.t1_ms), Speaker: int(c.speaker_id), P: float32(c.p)}
	}
	return out, nil
}

// Timings are the model load and the last run's stages.
func (d *DiarizeSession) Timings() Timings {
	var c C.struct_transcribe_timings
	C.transcribe_timings_init(&c)
	C.transcribe_diarize_get_timings(d.c, &c)
	return goTimings(&c)
}
