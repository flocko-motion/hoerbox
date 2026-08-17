// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox stream` — resolves a URL (or ./in/*.url) and plays every track in order
// limits:  thin wiring; streaming logic lives in internal/ytmusic and internal/pcmpipe
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
	"github.com/flocko-motion/hoerbox/internal/pcmpipe"
	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

// naturalizeFraction is the minimum share of a track's own runtime hoerbox
// waits before starting the next download — see naturalizeWait.
const naturalizeFraction = 0.5

var (
	streamOutput  string
	streamInput   string
	streamFast    bool
	streamRefresh bool
	streamBrowser string
)

var streamCmd = &cobra.Command{
	Use:   "stream [youtube-music-url]",
	Short: "Resolve a track/playlist/album URL (or ./in/*.url) and play every track in order",
	Long: `Resolves a track, playlist, or album URL the same way "resolve" does,
then plays each resolved track in turn: prints its metadata, streams it
(yt-dlp fetches, ffmpeg decodes to PCM) to completion, then moves to the
next. Exits once the whole list has played.

With no URL argument, hoerbox reads every "*.url" file in --input
(default ./in/) instead — one URL per file, e.g. "in/sandmaennchen.url"
containing a single playlist link — and works through all of them as one
combined list. This is the low-friction way to build up a local library:
drop a .url file in for each playlist you want, then just run "hoerbox
stream".

Every track's download is recorded to download-log.json in --output, so a
track already marked "success" there is never re-fetched — re-running
"stream" after a partial/failed run naturally retries only what's still
missing. Use "hoerbox stats" to see that breakdown without downloading
anything.

Between any two actual download attempts (regardless of source or output
mode), hoerbox waits at least 50% of the previous track's own runtime
before starting the next — YouTube has no equivalent of the old
Spotify-side rate limit we ran into, and this is intentional insurance
against ever looking like a scraper. Pass --fast to disable this for a
quick smoke test.

yt-dlp/ffmpeg's own progress prints live by default — the closest thing to
a position indicator right now; there's no daemon exposing "current
playback position" to poll the way the old Spotify-based player had.
Pass -v for lower-level debug detail on top of that.

--output picks one of two modes:

  - a directory (the default: ./out/): "Album/NNN - Artist - Trackname.wav"
    under it, self-describing (double-click playable, no format flags
    needed) — the layout Hörbert-style players expect, ordering each
    folder's playback by the numeric prefix. Simplest mode: each track is
    its own subprocess writing directly to its own file, no FIFO involved.

  - a file/FIFO path: one continuous raw PCM stream for the whole run,
    meant to be handed off live to a reader (the sibling audio-output
    project, or ffplay for local listening during development):

        mkfifo /tmp/hoerbox.pcm   # once, if not already a FIFO
        ffplay -f s16le -ar 44100 -ac 2 -i /tmp/hoerbox.pcm &
        hoerbox stream --output /tmp/hoerbox.pcm https://music.youtube.com/playlist?list=...

    Each track is still its own subprocess pair opening/closing that same
    FIFO path in turn (not one continuous decode) — see internal/pcmpipe
    for how a brief gap between tracks is kept from looking like the
    stream ending to whatever's reading it. There's no download-log here:
    nothing is written to disk to check a track's status against.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if streamBrowser != "" {
			cfg.YouTube.CookiesFromBrowser = streamBrowser
		}

		var tracks []ytmusic.Track
		var err error
		if len(args) == 1 {
			tracks, err = resolveCached(ctx, urlCachePath(args[0]), args[0], streamRefresh)
		} else {
			tracks, err = resolveInputDir(ctx, streamInput, streamRefresh)
		}
		if err != nil {
			return err
		}
		if len(tracks) == 0 {
			return fmt.Errorf("resolved zero playable tracks")
		}

		if isDirectoryOutput(streamOutput) {
			printLine("output target %q is a directory: writing one file per track", streamOutput)
			return runStreamToDirectory(ctx, streamOutput, tracks, streamFast)
		}

		printLine("output target %q is a file/FIFO: writing one continuous PCM stream", streamOutput)
		return runStreamToPipe(ctx, streamOutput, tracks, streamFast)
	},
}

// resolveInputDir resolves every "*.url" file in dir into one combined
// track list, in filename order, via resolveCached.
func resolveInputDir(ctx context.Context, dir string, refresh bool) ([]ytmusic.Track, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating input directory %s: %w", dir, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading input directory %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".url") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no *.url files found in %s", dir)
	}

	var all []ytmusic.Track
	for _, name := range names {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		rawURL := strings.TrimSpace(string(content))
		tracks, err := resolveCached(ctx, cachePathFor(path), rawURL, refresh)
		if err != nil {
			return nil, err
		}
		all = append(all, tracks...)
	}
	return all, nil
}

// runStreamToDirectory is the debug-dump mode: one file per track, each
// its own subprocess. Tracks already a success in download-log.json are
// skipped.
func runStreamToDirectory(ctx context.Context, dir string, tracks []ytmusic.Track, fast bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	mlog := downloadlog.Open(dir)
	trackNos := albumTrackNumbers(tracks)

	var prevTrack *ytmusic.Track
	var prevStart time.Time
	succeeded, failed, skipped := 0, 0, 0

	printProgress := func() {
		untried := len(tracks) - skipped - succeeded - failed
		printLine("  progress: %d total, %d succeeded, %d failed, %d untried", len(tracks), skipped+succeeded, failed, untried)
	}

	for i, t := range tracks {
		e, ok, err := mlog.Get(t.URL)
		if err != nil {
			return fmt.Errorf("checking download log: %w", err)
		}
		if ok && e.Status == downloadlog.StatusSuccess {
			skipped++
			printProgress()
			continue
		}

		if !fast && prevTrack != nil {
			if err := naturalizeWait(ctx, prevTrack.LengthMs, prevStart); err != nil {
				return err
			}
		}

		relPath := trackOutputPath(trackNos[i], t)
		outPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("creating output subdirectory: %w", err)
		}
		printLine("[%d/%d] %s — %s (%s)\n  -> %s", i+1, len(tracks), t.TrackName, t.Artist, formatDuration(t.LengthMs), outPath)
		printToken("status", fmt.Sprintf("downloading %d/%d: %s — %s", i+1, len(tracks), t.TrackName, t.Artist))

		start := time.Now()
		streamErr := ytmusic.StreamTrack(ctx, log, cfg, t.URL, outPath)
		prevTrack, prevStart = &t, start
		if streamErr != nil && ctx.Err() != nil {
			return ctx.Err()
		}

		entry := downloadlog.Entry{
			URL: t.URL, TrackName: t.TrackName, Artist: t.Artist, Album: t.Album,
			File: relPath, AttemptedAt: start,
		}
		if streamErr != nil {
			entry.Status, entry.Error = downloadlog.StatusFailed, streamErr.Error()
			printLine("  ! failed: %s", streamErr)
			failed++
		} else {
			entry.Status = downloadlog.StatusSuccess
			succeeded++
		}
		printProgress()
		if err := mlog.Record(entry); err != nil {
			return fmt.Errorf("recording download log: %w", err)
		}
		printToken("refresh", "")
	}

	printLine("done: %d succeeded, %d failed, %d already downloaded", succeeded, failed, skipped)
	if failed > 0 {
		printLine("run \"hoerbox stream\" again to retry failed tracks, or \"hoerbox stats\" for a summary")
	}
	printToken("status", "idle")
	return nil
}

// runStreamToPipe is the continuous-hand-off mode: every track writes to
// the same FIFO in turn, kept alive by pcmpipe (see its doc comment).
func runStreamToPipe(ctx context.Context, fifoPath string, tracks []ytmusic.Track, fast bool) error {
	keepAlive, err := pcmpipe.Open(fifoPath)
	if err != nil {
		return fmt.Errorf("preparing output fifo: %w", err)
	}
	defer keepAlive.Close()

	var prevTrack *ytmusic.Track
	var prevStart time.Time

	for i, t := range tracks {
		if !fast && prevTrack != nil {
			if err := naturalizeWait(ctx, prevTrack.LengthMs, prevStart); err != nil {
				return err
			}
		}

		printLine("[%d/%d] %s — %s (%s)", i+1, len(tracks), t.TrackName, t.Artist, formatDuration(t.LengthMs))

		start := time.Now()
		streamErr := ytmusic.StreamTrack(ctx, log, cfg, t.URL, fifoPath)
		prevTrack, prevStart = &t, start
		if streamErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			printLine("  ! failed: %s — skipping", streamErr)
		}
	}

	printLine("done")
	return nil
}

// naturalizeWait blocks until at least naturalizeFraction of prevLengthMs
// has elapsed since prevStart — see the package doc comment on why.
func naturalizeWait(ctx context.Context, prevLengthMs int, prevStart time.Time) error {
	minGap := time.Duration(float64(prevLengthMs)*naturalizeFraction) * time.Millisecond
	remaining := minGap - time.Since(prevStart)
	if remaining <= 0 {
		return nil
	}

	printLine("naturalizer: waiting %s before the next request (--fast to disable)", remaining.Round(time.Second))
	printToken("status", fmt.Sprintf("waiting %s before the next download...", remaining.Round(time.Second)))
	select {
	case <-time.After(remaining):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isDirectoryOutput reports whether --output names a directory: it exists
// as one, or doesn't exist yet but ends in a path separator.
func isDirectoryOutput(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return info.IsDir()
	}
	return strings.HasSuffix(path, string(os.PathSeparator))
}

// albumTrackNumbers ranks each track 1-indexed within its own album, not
// its position in the run's flattened multi-album slice, and independent
// of which tracks are skipped as already-downloaded.
func albumTrackNumbers(tracks []ytmusic.Track) []int {
	nos := make([]int, len(tracks))
	seen := make(map[string]int, len(tracks))
	for i, t := range tracks {
		key := sanitizeFilenameComponent(t.Album)
		seen[key]++
		nos[i] = seen[key]
	}
	return nos
}

// trackOutputPath builds a relative "Album/NNN - Artist - Trackname.wav"
// path — one folder per album, artist in the filename since a compilation
// album's tracks don't share one. Empty fields are dropped.
func trackOutputPath(trackNo int, t ytmusic.Track) string {
	folder := sanitizeFilenameComponent(t.Album)
	if folder == "" {
		folder = "untitled"
	}

	nameParts := make([]string, 0, 2)
	for _, p := range []string{t.Artist, t.TrackName} {
		if s := sanitizeFilenameComponent(p); s != "" {
			nameParts = append(nameParts, s)
		}
	}
	filename := fmt.Sprintf("%03d - %s.wav", trackNo, strings.Join(nameParts, " - "))
	return filepath.Join(folder, filename)
}

var filenameReplacer = strings.NewReplacer(
	"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-",
)

func sanitizeFilenameComponent(s string) string {
	return strings.TrimSpace(filenameReplacer.Replace(s))
}

func formatDuration(ms int) string {
	totalSeconds := ms / 1000
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	return fmt.Sprintf("%dm%02ds", totalSeconds/60, totalSeconds%60)
}

func init() {
	streamCmd.Flags().StringVar(&streamOutput, "output", "./out/", "output target; a directory (default) writes one file per track, a file/FIFO path writes one continuous PCM stream")
	streamCmd.Flags().StringVar(&streamInput, "input", "./in/", "directory of *.url files to resolve when no URL argument is given")
	streamCmd.Flags().BoolVar(&streamFast, "fast", false, "disable the naturalizer pacing between downloads — for a quick smoke test, not routine use")
	streamCmd.Flags().BoolVar(&streamRefresh, "refresh", false, "re-fetch URL resolution instead of using a cached one")
	streamCmd.Flags().StringVar(&streamBrowser, "browser", "", "read cookies from this local browser instead of the default (vivaldi)")

	rootCmd.AddCommand(streamCmd)
}
