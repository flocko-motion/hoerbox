// package: ytmusic / integration
// type:    logic
// job:     machine-readable download progress out of yt-dlp
// limits:  byte counts only; yt-dlp's own speed/ETA estimates aren't passed on
package ytmusic

import (
	"strconv"
	"strings"
)

// ProgressFunc receives a download's progress in bytes; size is 0 while
// yt-dlp doesn't know it yet. May be nil.
type ProgressFunc func(downloaded, size int64)

// progressPrefix marks the progress lines progressArgs asks yt-dlp for,
// so tailLogger can tell them from its normal output.
const progressPrefix = "hoerbox-progress "

// progressArgs makes yt-dlp print one parseable line per progress update.
var progressArgs = []string{
	"--newline",
	"--progress-template",
	"download:" + progressPrefix + "%(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s",
}

// parseProgress parses a progressArgs line. Fields yt-dlp doesn't know
// print as "NA" (or "None"); the exact total is preferred over the
// estimate.
func parseProgress(line string) (downloaded, size int64, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), progressPrefix)
	if !found {
		return 0, 0, false
	}
	fields := strings.Fields(rest)
	num := func(i int) int64 {
		if i >= len(fields) {
			return 0
		}
		f, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return 0
		}
		return int64(f)
	}
	size = num(1)
	if size == 0 {
		size = num(2)
	}
	return num(0), size, true
}
