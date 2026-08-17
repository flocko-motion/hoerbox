// package: pcmpipe / integration
// type:    logic
// job:     keeps a FIFO from producing a spurious EOF between per-track writers
// limits:  doesn't read or write PCM itself — see doc comment on KeepAlive
package pcmpipe

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// KeepAlive holds a FIFO's fd open O_RDWR so it always has a writer: each
// track writes via its own yt-dlp|ffmpeg pair, and the gap between one
// closing and the next opening would otherwise read as EOF to the
// downstream reader. It never reads or writes through the fd itself.
type KeepAlive struct {
	file *os.File
}

// Open creates path as a FIFO if missing and holds it open.
func Open(path string) (*KeepAlive, error) {
	if err := ensureFifo(path); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening fifo %s: %w", path, err)
	}
	return &KeepAlive{file: f}, nil
}

// Close releases the held fd; the FIFO itself is left on disk.
func (k *KeepAlive) Close() error {
	return k.file.Close()
}

func ensureFifo(path string) error {
	info, err := os.Stat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeNamedPipe == 0 {
			return fmt.Errorf("%s already exists and is not a FIFO", path)
		}
		return nil
	case os.IsNotExist(err):
		return unix.Mkfifo(path, 0o600)
	default:
		return fmt.Errorf("checking %s: %w", path, err)
	}
}
