// package: cmd / cli
// type:    logic
// job:     a "stream" run's one place to report state: data/status.json plus GUI token lines
// limits:  one reporter per process; status writes are best-effort (logged, never fatal)
package cmd

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/flocko-motion/hoerbox/internal/runstate"
)

// reporter keeps data/status.json current for "hoerbox status" and the
// GUI, and mirrors each change as a token line for a GUI driving this
// run over its stdout.
type reporter struct {
	dir string
	st  runstate.Status
}

// rep is the running "stream"'s reporter — set in its RunE, like cfg/log
// in root.go's PersistentPreRunE.
var rep *reporter

func newReporter(dir string) *reporter {
	r := &reporter{dir: dir, st: runstate.Status{
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}}
	// The last error outlives the run that hit it: that's the point of
	// asking for it after the fact.
	if prev, ok, err := runstate.Read(dir); err == nil && ok {
		r.st.LastError = prev.LastError
	}
	r.phase(runstate.PhaseStarting, "starting")
	return r
}

// phase moves the run to phase, showing msg as its one-line description.
func (r *reporter) phase(phase, msg string) {
	r.st.Phase, r.st.Message = phase, msg
	if phase != runstate.PhaseDownloading {
		r.st.Track = nil
	}
	r.flush()
	printToken("status", msg)
}

// track marks t as the one now downloading.
func (r *reporter) track(t runstate.Track, msg string) {
	r.st.Track = &t
	r.phase(runstate.PhaseDownloading, msg)
}

func (r *reporter) counts(c runstate.Counts) {
	r.st.Counts = c
	r.flush()
}

// error records msg as the last error; track is "" for a run-level one.
func (r *reporter) error(track, msg string) {
	r.st.LastError = &runstate.Error{At: time.Now(), Track: track, Message: msg}
	r.flush()
	printToken("error", msg)
}

// finish records how the run ended, given RunE's own return value.
func (r *reporter) finish(err error) {
	switch {
	case err == nil:
		if r.st.Phase != runstate.PhaseDone {
			r.phase(runstate.PhaseDone, "done")
		}
	case errors.Is(err, context.Canceled):
		r.phase(runstate.PhaseStopped, "stopped")
	default:
		r.error("", err.Error())
		r.phase(runstate.PhaseFailed, "failed: "+err.Error())
	}
}

func (r *reporter) flush() {
	if err := runstate.Write(r.dir, r.st); err != nil {
		log.Debugf("updating status: %s", err)
	}
}
