// package: config / integration
// type:    logic
// job:     hoerbox's own settings — hardcoded, not file-based (private, single-user project)
// limits:  edit New() and rebuild to change anything; no per-deployment config file
package config

import (
	"fmt"
	"os"
)

// Config is hoerbox's own configuration. Hardcoded rather than loaded from
// a file: this is a private, single-user project with exactly one owner
// and one real setting, so a config file format buys nothing a rebuild
// doesn't already give for free. --cookies-file/--cookies-from-browser
// remain available as CLI flags for one-off overrides without a rebuild.
type Config struct {
	// DataDir stores any local state (currently just download/temp scratch
	// space; nothing persistent is required for YouTube Music auth).
	DataDir string

	// LogLevel is one of: trace, debug, info, warn, error.
	LogLevel string

	Audio   Audio
	YouTube YouTube
}

// Audio configures how decoded PCM leaves hoerbox. hoerbox's own job stops
// at handing off raw PCM; a separate project owns actual sound output.
type Audio struct {
	// OutputFormat is one of: s16le, s32le, f32le. hoerbox targets s16le.
	OutputFormat string
	// SampleRate and Channels describe the PCM format ffmpeg is asked to
	// produce; s16le/44100/2 (CD-quality stereo) is what the sibling
	// audio-output project expects.
	SampleRate int
	Channels   int
}

// YouTube configures how hoerbox authenticates to yt-dlp for account-gated
// content — turned out to be most real tracks, not just an edge case.
type YouTube struct {
	// CookiesFile is a Netscape-format cookies.txt exported from a
	// logged-in browser session. Works headless — this is what the Pi
	// will use.
	CookiesFile string
	// CookiesFromBrowser reads cookies straight from a local browser's
	// profile (e.g. "vivaldi"). Convenient for development; not usable on
	// a headless Pi.
	CookiesFromBrowser string
}

// New returns hoerbox's configuration.
func New() *Config {
	return &Config{
		DataDir:  "./data",
		LogLevel: "info",
		Audio: Audio{
			OutputFormat: "s16le",
			SampleRate:   44100,
			Channels:     2,
		},
		YouTube: YouTube{
			CookiesFromBrowser: "vivaldi",
		},
	}
}

// EnsureDataDir creates DataDir if it doesn't exist yet.
func (c *Config) EnsureDataDir() error {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	return nil
}
