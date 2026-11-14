package runstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Boost shortens the pause between downloads for one album, for when
// it's needed now. It's a file (data/boost), so it can be started from
// the CLI or GUI while a run is going; the run picks it up on its next
// tick, and ends it once it moves on from that album.
type Boost struct {
	// Album is the album (./in/ folder name) being boosted. Empty means
	// "whichever album the run downloads next" — the run fills it in.
	Album string    `json:"album"`
	Since time.Time `json:"since"`
}

const boostName = "boost"

// SetBoost turns boost on for b.Album ("" for the next album downloaded).
func SetBoost(dir string, b Boost) error {
	if b.Since.IsZero() {
		b.Since = time.Now()
	}
	content, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("starting boost: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, boostName), content, 0o644); err != nil {
		return fmt.Errorf("starting boost: %w", err)
	}
	return nil
}

// ClearBoost turns boost off.
func ClearBoost(dir string) error {
	if err := os.Remove(filepath.Join(dir, boostName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("ending boost: %w", err)
	}
	return nil
}

// ReadBoost returns the current boost; ok is false if none is on.
func ReadBoost(dir string) (b Boost, ok bool) {
	content, err := os.ReadFile(filepath.Join(dir, boostName))
	if err != nil {
		return Boost{}, false
	}
	if err := json.Unmarshal(content, &b); err != nil {
		return Boost{}, false // an older/foreign format: treat as off
	}
	return b, true
}
