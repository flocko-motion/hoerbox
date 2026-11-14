// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox stream` — resolves a URL (or every album in ./in/) and plays every track in order
// limits:  thin wiring; streaming logic lives in internal/ytmusic and internal/pcmpipe
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
	"github.com/flocko-motion/hoerbox/internal/pcmpipe"
	"github.com/flocko-motion/hoerbox/internal/runstate"
	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

// naturalizeFraction is the minimum share of a track's own runtime hoerbox
// waits before starting the next download — see naturalizeWait.
const naturalizeFraction = 0.5

// maxConsecutiveFailures re-checks hoerbox's own setup (see checkSetup)
// after this many failures in a row, even if each looked track-specific.
const maxConsecutiveFailures = 3

var (
	streamOutput  string
	streamInput   string
	streamFast    bool
	streamRefresh bool
	streamBrowser string
	streamFormat  string
)

var streamCmd = &cobra.Command{
	Use:   "stream [youtube-music-url]",
	Short: "Resolve a track/playlist/album URL (or every album in ./in/) and play every track in order",
	Long: `Resolves a track, playlist, or album URL the same way "resolve" does,
then plays each resolved track in turn: prints its metadata, streams it
(yt-dlp fetches, ffmpeg decodes to PCM) to completion, then moves to the
next. Exits once the whole list has played.

With no URL argument, hoerbox reads every album in --input (default
./in/) instead — one folder per album, e.g. "in/Sandmännchen - Folge 26/"
whose album.url holds the album's playlist link — and works through all
of them as one combined list. This is the low-friction way to build up a
local library: "hoerbox add <url>" (or the GUI's Add) for each album you
want, then just run "hoerbox stream". A bare "<name>.url" file dropped
into ./in/ by hand works too: it's moved into its own folder. Albums paused with "hoerbox pause" are skipped; pausing or
cancelling an album while it's downloading abandons that download.

Only one "stream" runs at a time — a second one refuses to start. While
it runs, "hoerbox status" shows what it's doing, and the last error any
run hit.

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

  - a directory (the default: ./out/): "Album/NNN - Artist - Trackname.ogg"
    under it — the archive. Each file is YouTube's own Opus stream,
    untouched (no re-encoding), tagged with title/artist/album/track
    number; the hörbert desktop software takes these as they are and
    converts them for the player. --format wav writes decoded WAV
    instead (~5x larger, untagged). Each folder's playback order is the
    numeric prefix. Simplest mode: each track is its own subprocess
    writing directly to its own file, no FIFO involved.

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
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx := cmd.Context()
		if streamBrowser != "" {
			cfg.YouTube.CookiesFromBrowser = streamBrowser
		}
		switch streamFormat {
		case "":
		case ytmusic.ArchiveFormatOgg, ytmusic.ArchiveFormatWAV:
			cfg.Audio.ArchiveFormat = streamFormat
		default:
			return fmt.Errorf("--format must be %q or %q, not %q", ytmusic.ArchiveFormatOgg, ytmusic.ArchiveFormatWAV, streamFormat)
		}

		lock, err := runstate.Acquire(cfg.DataDir)
		if err != nil {
			return err
		}
		defer lock.Release()
		// Only now, holding the lock, is it safe to move old flat-layout
		// albums into folders (see albumURLFiles).
		if err := migrateFlatInput(streamInput); err != nil {
			return err
		}
		rep = newReporter(cfg.DataDir)
		defer func() { rep.finish(err) }()

		rep.phase(runstate.PhaseResolving, "resolving albums...")
		var tracks []queuedTrack
		var paused []string
		if len(args) == 1 {
			var resolved []ytmusic.Track
			resolved, err = resolveCached(ctx, urlCachePath(args[0]), args[0], streamRefresh)
			tracks = queueTracks(resolved, "")
		} else {
			tracks, paused, err = resolveInputDir(ctx, streamInput, streamRefresh)
		}
		if err != nil {
			return err
		}
		for _, name := range paused {
			printLine("album paused, its tracks will be skipped: %s", name)
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

// queuedTrack is a track plus the album (its ./in/<album>/album.url) it's queued
// from — "" for a URL given straight on the command line.
type queuedTrack struct {
	ytmusic.Track
	Source string
}

func plainTracks(queued []queuedTrack) []ytmusic.Track {
	tracks := make([]ytmusic.Track, len(queued))
	for i, q := range queued {
		tracks[i] = q.Track
	}
	return tracks
}

func queueTracks(tracks []ytmusic.Track, source string) []queuedTrack {
	queued := make([]queuedTrack, len(tracks))
	for i, t := range tracks {
		queued[i] = queuedTrack{Track: t, Source: source}
	}
	return queued
}

// resolveInputDir resolves every album in dir into one combined track
// list, in album-name order, via resolveCached. Paused albums are
// included — leaving them out would shift albumTrackNumbers for any
// other album sharing their album name — and also listed by name in
// paused; skipping their tracks is the caller's call (albumQueueState).
func resolveInputDir(ctx context.Context, dir string, refresh bool) (tracks []queuedTrack, paused []string, err error) {
	paths, err := albumURLFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("no albums found in %s (add one with \"hoerbox add <url>\")", dir)
	}

	for _, path := range paths {
		if albumQueueState(path) == "paused" {
			paused = append(paused, albumName(path))
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", path, err)
		}

		rawURL := strings.TrimSpace(string(content))
		resolved, err := resolveCached(ctx, cachePathFor(path), rawURL, refresh)
		if err != nil {
			return nil, nil, err
		}
		tracks = append(tracks, queueTracks(resolved, path)...)
	}
	return tracks, paused, nil
}

// errAlbumPaused and errAlbumCancelled are why watchAlbum abandoned a
// track's download.
var (
	errAlbumPaused    = errors.New("album paused")
	errAlbumCancelled = errors.New("album cancelled")
)

// watchAlbum cancels a track's download (with errAlbumPaused or
// errAlbumCancelled as the cause) the moment its album is paused or
// cancelled. Returns once ctx is done.
func watchAlbum(ctx context.Context, cancel context.CancelCauseFunc, source string) {
	if source == "" {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			switch albumQueueState(source) {
			case "paused":
				cancel(errAlbumPaused)
				return
			case "cancelled":
				cancel(errAlbumCancelled)
				return
			}
		}
	}
}

// downloadTrack fetches one track to outPath — an archived .ogg (see
// ytmusic.ArchiveTrack), else decoded PCM — abandoning it if the track's
// album is paused or cancelled meanwhile: dequeued then reports why, and
// err is meaningless.
func downloadTrack(ctx context.Context, t queuedTrack, trackNo int, outPath string) (dequeued, err error) {
	trackCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go watchAlbum(trackCtx, cancel, t.Source)

	if strings.HasSuffix(outPath, "."+ytmusic.ArchiveFormatOgg) {
		tags := ytmusic.ArchiveTags{Title: t.TrackName, Artist: t.Artist, Album: t.Album, TrackNo: trackNo}
		err = ytmusic.ArchiveTrack(trackCtx, log, cfg, t.URL, outPath, tags, rep.progress)
	} else {
		err = ytmusic.StreamTrack(trackCtx, log, cfg, t.URL, outPath, rep.progress)
	}
	if cause := context.Cause(trackCtx); errors.Is(cause, errAlbumPaused) || errors.Is(cause, errAlbumCancelled) {
		return cause, nil
	}
	return nil, err
}

// skipNotice prints "skipping <album> (paused)" once per album per run,
// not once per track.
type skipNotice map[string]bool

func (n skipNotice) note(t queuedTrack, state string) {
	if n[t.Source] {
		return
	}
	n[t.Source] = true
	printLine("skipping album %s (%s)", albumName(t.Source), state)
}

func trackStatus(i int, t queuedTrack) runstate.Track {
	st := runstate.Track{Index: i + 1, Name: t.TrackName, Artist: t.Artist, URL: t.URL}
	if t.Source != "" {
		st.Playlist = albumName(t.Source)
	}
	return st
}

// runStreamToDirectory is the debug-dump mode: one file per track, each
// its own subprocess. Tracks already a success in download-log.json are
// skipped.
func runStreamToDirectory(ctx context.Context, dir string, tracks []queuedTrack, fast bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	mlog := downloadlog.Open(dir)
	trackNos := albumTrackNumbers(plainTracks(tracks))

	var prevTrack *queuedTrack
	var prevStart time.Time
	c := runstate.Counts{Total: len(tracks)}
	consecutiveFailures := 0
	// setupVerified: yt-dlp+cookies are known to work right now (a track
	// succeeded, or a probe passed), so a failure is the track's own fault.
	setupVerified := false
	skipped := skipNotice{}

	printProgress := func() {
		rep.counts(c)
		untried := c.Total - c.Skipped - c.Succeeded - c.Failed - c.Paused - c.Unavailable
		printLine("  progress: %d total, %d succeeded, %d failed, %d unavailable, %d paused, %d untried", c.Total, c.Skipped+c.Succeeded, c.Failed, c.Unavailable, c.Paused, untried)
	}

	for i, t := range tracks {
		e, ok, err := mlog.Get(t.URL)
		if err != nil {
			return fmt.Errorf("checking download log: %w", err)
		}
		if ok && e.Status == downloadlog.StatusSuccess {
			c.Skipped++
			printProgress()
			continue
		}
		if ok && e.Status == downloadlog.StatusUnavailable {
			c.Unavailable++
			printProgress()
			continue
		}
		if state := albumQueueState(t.Source); state != "" {
			skipped.note(t, state)
			c.Paused++
			rep.counts(c)
			continue
		}

		if !fast && prevTrack != nil {
			if err := naturalizeWait(ctx, *prevTrack, prevStart, t); err != nil {
				return err
			}
			// The wait can be long: check again.
			if state := albumQueueState(t.Source); state != "" {
				skipped.note(t, state)
				c.Paused++
				rep.counts(c)
				continue
			}
		}

		relPath := trackOutputPath(trackNos[i], t.Track, cfg.Audio.ArchiveFormat)
		outPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("creating output subdirectory: %w", err)
		}
		printLine("[%d/%d] %s — %s (%s)\n  -> %s", i+1, len(tracks), t.TrackName, t.Artist, formatDuration(t.LengthMs), outPath)
		rep.track(trackStatus(i, t), fmt.Sprintf("downloading %d/%d: %s — %s", i+1, len(tracks), t.TrackName, t.Artist))

		start := time.Now()
		dequeued, streamErr := downloadTrack(ctx, t, trackNos[i], outPath)
		prevTrack, prevStart = &t, start
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if dequeued != nil {
			_ = os.Remove(outPath) // a partial file, not a track
			printLine("  abandoned: %s", dequeued)
			skipped.note(t, strings.TrimPrefix(dequeued.Error(), "album "))
			c.Paused++
			printProgress()
			printToken("refresh", "")
			continue
		}

		entry := downloadlog.Entry{
			URL: t.URL, TrackName: t.TrackName, Artist: t.Artist, Album: t.Album,
			File: relPath, AttemptedAt: start,
		}
		var setupErr error
		if streamErr == nil {
			entry.Status = downloadlog.StatusSuccess
			c.Succeeded++
			consecutiveFailures = 0
			setupVerified = true
		} else {
			printLine("  ! failed: %s", streamErr)
			rep.error(t.TrackName+" — "+t.Artist, streamErr.Error())
			consecutiveFailures++
			kind := ytmusic.KindOf(streamErr)
			if kind == ytmusic.FailureBlocked || consecutiveFailures >= maxConsecutiveFailures {
				setupVerified = false
			}
			if !setupVerified {
				setupErr = checkSetup(ctx, streamErr)
				setupVerified = setupErr == nil
				consecutiveFailures = 0
			}
			entry.Error = streamErr.Error()
			if setupVerified && kind == ytmusic.FailureUnavailable {
				// Recorded so later runs skip it instead of re-failing on it.
				entry.Status = downloadlog.StatusUnavailable
				printLine("  unavailable on YouTube (removed or region-blocked): won't retry it")
				c.Unavailable++
			} else {
				entry.Status = downloadlog.StatusFailed
				c.Failed++
			}
		}
		printProgress()
		if err := mlog.Record(entry); err != nil {
			return fmt.Errorf("recording download log: %w", err)
		}
		printToken("refresh", "")
		if setupErr != nil {
			return setupErr
		}
	}

	summary := fmt.Sprintf("done: %d succeeded, %d failed, %d unavailable, %d already downloaded, %d paused", c.Succeeded, c.Failed, c.Unavailable, c.Skipped, c.Paused)
	printLine("%s", summary)
	if c.Failed > 0 {
		printLine("run \"hoerbox stream\" again to retry failed tracks, or \"hoerbox stats\" for a summary")
	}
	rep.phase(runstate.PhaseDone, summary)
	return nil
}

// checkSetup runs ytmusic.Probe after a failure to tell a dead track from
// a broken setup. A broken setup stops the run with the concrete fix,
// since every further track would fail the same way.
func checkSetup(ctx context.Context, lastErr error) error {
	printLine("  checking whether yt-dlp and the cookies work at all...")
	rep.phase(runstate.PhaseDownloading, "checking yt-dlp and cookies...")
	d := ytmusic.Probe(ctx, log, cfg)
	if d.Healthy {
		printLine("  yt-dlp and cookies work: this failure is the track's own, carrying on")
		return nil
	}
	printLine("! stopping: %s", d.Fix)
	return fmt.Errorf("download setup is broken (%s) — last: %w", firstLine(d.Fix), lastErr)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// runStreamToPipe is the continuous-hand-off mode: every track writes to
// the same FIFO in turn, kept alive by pcmpipe (see its doc comment).
func runStreamToPipe(ctx context.Context, fifoPath string, tracks []queuedTrack, fast bool) error {
	keepAlive, err := pcmpipe.Open(fifoPath)
	if err != nil {
		return fmt.Errorf("preparing output fifo: %w", err)
	}
	defer keepAlive.Close()

	var prevTrack *queuedTrack
	var prevStart time.Time
	c := runstate.Counts{Total: len(tracks)}
	skipped := skipNotice{}

	for i, t := range tracks {
		if state := albumQueueState(t.Source); state != "" {
			skipped.note(t, state)
			c.Paused++
			rep.counts(c)
			continue
		}
		if !fast && prevTrack != nil {
			if err := naturalizeWait(ctx, *prevTrack, prevStart, t); err != nil {
				return err
			}
		}

		printLine("[%d/%d] %s — %s (%s)", i+1, len(tracks), t.TrackName, t.Artist, formatDuration(t.LengthMs))
		rep.track(trackStatus(i, t), fmt.Sprintf("playing %d/%d: %s — %s", i+1, len(tracks), t.TrackName, t.Artist))

		start := time.Now()
		dequeued, streamErr := downloadTrack(ctx, t, i+1, fifoPath)
		prevTrack, prevStart = &t, start
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case dequeued != nil:
			printLine("  abandoned: %s", dequeued)
			c.Paused++
		case streamErr != nil:
			printLine("  ! failed: %s — skipping", streamErr)
			rep.error(t.TrackName+" — "+t.Artist, streamErr.Error())
			c.Failed++
		default:
			c.Succeeded++
		}
		rep.counts(c)
	}

	printLine("done")
	rep.phase(runstate.PhaseDone, fmt.Sprintf("done: %d played, %d failed, %d paused", c.Succeeded, c.Failed, c.Paused))
	return nil
}

// naturalizeWait blocks until at least naturalizeFraction of prev's
// length has elapsed since prevStart — see the package doc comment on
// why — or boostFraction while next's album is boosted ("hoerbox
// boost"). Both are re-checked once a second, so a boost started mid-wait
// cuts the wait short, and the GUI shows a live countdown.
func naturalizeWait(ctx context.Context, prev queuedTrack, prevStart time.Time, next queuedTrack) error {
	deadline := func() (time.Time, string) {
		fraction, boosted := naturalizeFraction, boostFor(prev, next)
		if boosted != "" {
			fraction = boostFraction
		}
		return prevStart.Add(time.Duration(float64(prev.LengthMs)*fraction) * time.Millisecond), boosted
	}

	until, _ := deadline()
	if time.Until(until) <= 0 {
		return nil
	}
	waitFrom := time.Now()
	printLine("naturalizer: waiting %s before the next request (\"hoerbox boost\" to shorten, --fast to disable)", time.Until(until).Round(time.Second))

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		until, boosted := deadline()
		remaining := time.Until(until).Round(time.Second)
		if remaining <= 0 {
			return nil
		}
		msg := fmt.Sprintf("waiting %s before the next download...", remaining)
		if boosted != "" {
			msg = fmt.Sprintf("waiting %s before the next download (boosting %s)...", remaining, boosted)
		}
		rep.wait(waitFrom, until, msg)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// boostFor returns the boosted album's name if the pause before next
// should be a boosted one, else "". A boost for "the next album" is
// bound to next's album here. A boost whose album prev was from, but
// next isn't, is over — the run has moved on — and is ended.
func boostFor(prev, next queuedTrack) string {
	b, ok := runstate.ReadBoost(cfg.DataDir)
	if !ok {
		return ""
	}
	nextAlbum := sourceAlbum(next)
	if b.Album == "" {
		b.Album = nextAlbum
		if err := runstate.SetBoost(cfg.DataDir, b); err != nil {
			log.Debugf("binding boost: %s", err)
		}
	}

	switch b.Album {
	case nextAlbum:
		return b.Album
	case sourceAlbum(prev):
		if err := runstate.ClearBoost(cfg.DataDir); err != nil {
			log.Debugf("ending boost: %s", err)
		}
		printLine("boost ended: done with %s", b.Album)
		return ""
	default:
		return "" // not reached yet
	}
}

// sourceAlbum is t's album name, or "" for a URL given on the command line.
func sourceAlbum(t queuedTrack) string {
	if t.Source == "" {
		return ""
	}
	return albumName(t.Source)
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

// trackOutputPath builds a relative "Album/NNN - Artist - Trackname.<ext>"
// path — one folder per album, artist in the filename since a compilation
// album's tracks don't share one. Empty fields are dropped.
func trackOutputPath(trackNo int, t ytmusic.Track, ext string) string {
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
	filename := fmt.Sprintf("%03d - %s.%s", trackNo, strings.Join(nameParts, " - "), strings.TrimPrefix(ext, "."))
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
	streamCmd.Flags().StringVar(&streamInput, "input", "./in/", "directory of album folders to resolve when no URL argument is given")
	streamCmd.Flags().BoolVar(&streamFast, "fast", false, "disable the naturalizer pacing between downloads — for a quick smoke test, not routine use")
	streamCmd.Flags().BoolVar(&streamRefresh, "refresh", false, "re-fetch URL resolution instead of using a cached one")
	streamCmd.Flags().StringVar(&streamFormat, "format", "", "file format per track in directory output: ogg (default: YouTube's Opus stream, untouched and tagged) or wav")
	streamCmd.Flags().StringVar(&streamBrowser, "browser", "", "read cookies from this local browser instead of the default (vivaldi)")

	rootCmd.AddCommand(streamCmd)
}
