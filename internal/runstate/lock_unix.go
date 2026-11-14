//go:build unix

package runstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const lockName = "hoerbox.lock"

// Lock is a held single-instance lock. The kernel drops it the moment
// the holding process dies, however it dies, so a stale lock can't
// outlive its run the way a PID file can.
type Lock struct {
	f *os.File
}

// HeldError is Acquire's error when another process holds the lock.
type HeldError struct {
	PID int // 0 if unknown
}

func (e *HeldError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("hoerbox is already running (pid %d) — see \"hoerbox status\"", e.PID)
	}
	return "hoerbox is already running — see \"hoerbox status\""
}

// Acquire takes dir's single-instance lock, or returns a *HeldError if
// another process has it. It retries briefly first, so an IsRunning
// probe that happens to hold the lock for an instant isn't mistaken for
// a real run.
func Acquire(dir string) (*Lock, error) {
	path := filepath.Join(dir, lockName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	for attempt := 0; ; attempt++ {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if attempt == 5 {
			_ = f.Close()
			return nil, &HeldError{PID: readPID(path)}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Purely informational, for HeldError's message — the flock is the
	// actual lock.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &Lock{f: f}, nil
}

// Release drops the lock.
func (l *Lock) Release() {
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	_ = l.f.Close()
}

// IsRunning reports whether some process holds dir's lock right now.
func IsRunning(dir string) bool {
	f, err := os.Open(filepath.Join(dir, lockName))
	if err != nil {
		return false // never created: nothing has ever run
	}
	defer f.Close()

	// A shared probe: fails against a run's exclusive lock, and holds
	// nothing a run would notice beyond Acquire's own retry window.
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return true
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false
}

func readPID(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(content)))
	return pid
}
