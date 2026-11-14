// package: runstate / integration
// type:    logic
// job:     single-instance lock plus a status file, so anyone can see what "stream" is doing
// limits:  one status file per data dir; written atomically but not versioned/historical
package runstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Phases a Status can be in. starting/resolving/downloading/waiting are
// "active": seeing one with Running false means the run died without
// getting to say so (crash, kill -9, power loss).
const (
	PhaseStarting    = "starting"
	PhaseResolving   = "resolving"
	PhaseDownloading = "downloading"
	PhaseWaiting     = "waiting"
	PhaseDone        = "done"
	PhaseStopped     = "stopped"
	PhaseFailed      = "failed"
)

// Track is the track a run is currently working on.
type Track struct {
	Index    int    `json:"index"` // 1-based, within Counts.Total
	Name     string `json:"name"`
	Artist   string `json:"artist"`
	Playlist string `json:"playlist,omitempty"`
	URL      string `json:"url,omitempty"`
	// Downloaded and Size are the download's progress in bytes, as yt-dlp
	// reports it; Size is 0 while unknown.
	Downloaded int64 `json:"downloaded,omitempty"`
	Size       int64 `json:"size,omitempty"`
}

// Wait is the pause between two downloads: it began at From and ends at
// Until. Until can move mid-wait (a boost starting or ending).
type Wait struct {
	From  time.Time `json:"from"`
	Until time.Time `json:"until"`
}

// Counts is a run's progress so far.
type Counts struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"` // already downloaded before this run
	Paused    int `json:"paused"`  // skipped because their album is paused or was cancelled
	// Unavailable tracks YouTube no longer serves (removed, region-blocked).
	Unavailable int `json:"unavailable"`
}

// Error is the most recent error any run reported — carried over from
// run to run, so it survives the run that hit it.
type Error struct {
	At      time.Time `json:"at"`
	Track   string    `json:"track,omitempty"` // empty for a run-level error
	Message string    `json:"message"`
}

// Status is what data/status.json holds.
type Status struct {
	PID       int       `json:"pid"`
	StartedBy string    `json:"started_by"` // "gui" (a hoerbox window's own run) or "terminal"
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	Track     *Track    `json:"track,omitempty"`
	Wait      *Wait     `json:"wait,omitempty"` // only while Phase is PhaseWaiting
	Counts    Counts    `json:"counts"`
	LastError *Error    `json:"last_error,omitempty"`

	// Running is whether the run that wrote this still holds the lock.
	// Never stored; Read fills it in.
	Running bool `json:"running"`
}

// Active reports whether Phase is one a live run would be in.
func (s Status) Active() bool {
	switch s.Phase {
	case PhaseStarting, PhaseResolving, PhaseDownloading, PhaseWaiting:
		return true
	}
	return false
}

const statusName = "status.json"

// Write atomically replaces dir's status file with s (temp file + rename,
// so a reader never sees half a file).
func Write(dir string, s Status) error {
	s.UpdatedAt = time.Now()
	content, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling status: %w", err)
	}

	tmp, err := os.CreateTemp(dir, statusName+".*")
	if err != nil {
		return fmt.Errorf("writing status: %w", err)
	}
	_, werr := tmp.Write(content)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing status: %v", firstErr(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, statusName)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing status: %w", err)
	}
	return nil
}

// Read returns dir's status file, with Running filled in from the lock.
// ok is false if no run has ever written one.
func Read(dir string) (s Status, ok bool, err error) {
	content, err := os.ReadFile(filepath.Join(dir, statusName))
	switch {
	case os.IsNotExist(err):
		return Status{}, false, nil
	case err != nil:
		return Status{}, false, fmt.Errorf("reading status: %w", err)
	}
	if err := json.Unmarshal(content, &s); err != nil {
		return Status{}, false, fmt.Errorf("parsing status: %w", err)
	}
	s.Running = IsRunning(dir)
	return s, true, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
