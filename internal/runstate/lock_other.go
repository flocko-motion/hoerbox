//go:build !unix

package runstate

// Lock is a no-op on non-unix platforms: hoerbox only runs on macOS and
// Linux, and this just keeps the package building elsewhere.
type Lock struct{}

// HeldError is never returned here; it exists so callers build everywhere.
type HeldError struct {
	PID int
}

func (e *HeldError) Error() string { return "hoerbox is already running" }

// Acquire always succeeds on non-unix platforms.
func Acquire(string) (*Lock, error) { return &Lock{}, nil }

// Release does nothing.
func (*Lock) Release() {}

// IsRunning can't tell on non-unix platforms, and says no.
func IsRunning(string) bool { return false }
