// package: cmd / cli
// type:    logic
// job:     fails fast with a clear message if yt-dlp/ffmpeg/python3 aren't on PATH
// limits:  only checks presence on PATH, not version compatibility
package cmd

import (
	"fmt"
	"os/exec"
	"strings"
)

// checkDependencies verifies every external tool hoerbox shells out to is
// on PATH, returning one combined, actionable error rather than letting a
// confusing "exec: ... file not found" surface later from deep inside
// some subprocess call.
func checkDependencies() error {
	deps := []struct{ bin, hint string }{
		{"yt-dlp", "brew install yt-dlp  (or: pip install -U yt-dlp)"},
		{"ffmpeg", "brew install ffmpeg"},
		{"python3", "install Python 3 — yt-dlp needs it at runtime"},
	}

	var missing []string
	for _, d := range deps {
		if _, err := exec.LookPath(d.bin); err != nil {
			missing = append(missing, fmt.Sprintf("  %s: not found on PATH — %s", d.bin, d.hint))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required tool(s):\n%s", strings.Join(missing, "\n"))
}
