// package: cmd / cli
// type:    logic
// job:     computes per-album (./in/<album>/) status by cross-referencing the download log
// limits:  reads only cached resolutions — never hits the network
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

// PlaylistStatus summarizes one ./in/<album>/ folder: its tracks' download
// status, cross-referenced against outputDir's download-log.
type PlaylistStatus struct {
	Name      string    `json:"name"`     // the album folder's name (see albumName)
	Path      string    `json:"path"`     // the album's url file (see cmd/albumdir.go)
	AddedAt   time.Time `json:"added_at"` // the url file's mtime
	Resolved  bool      `json:"resolved"` // whether a cached resolution exists at all
	Paused    bool      `json:"paused"`   // see "hoerbox pause"
	Total     int       `json:"total"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	// Unavailable tracks YouTube no longer serves; they count as done.
	Unavailable int `json:"unavailable"`
	Untried     int `json:"untried"`
	// Missing is how many "succeeded" tracks have no file at their
	// recorded location any more (moved/deleted after the fact).
	Missing int `json:"missing"`

	// tracks backs Succeeded/Failed/... above and is what "clean" needs
	// each track's URL for; deliberately not part of the JSON a GUI
	// consumes (list --json), which only needs the counts.
	tracks []ytmusic.Track
}

// Done reports whether every track resolved and either succeeded (and
// still has its file) or is unavailable on YouTube — the condition "clean" removes a playlist under.
func (s PlaylistStatus) Done() bool {
	return s.Resolved && s.Total > 0 && s.Succeeded+s.Unavailable == s.Total && s.Failed == 0 && s.Missing == 0
}

// scanInputDir computes PlaylistStatus for every album in inputDir,
// newest-added first (see PlaylistStatus.AddedAt).
func scanInputDir(inputDir, outputDir string) ([]PlaylistStatus, error) {
	paths, err := albumURLFiles(inputDir)
	if err != nil {
		return nil, err
	}

	logEntries, err := downloadlog.Open(outputDir).All()
	if err != nil {
		return nil, fmt.Errorf("reading download log: %w", err)
	}

	var statuses []PlaylistStatus
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}

		status := PlaylistStatus{
			Name:    albumName(path),
			Path:    path,
			AddedAt: info.ModTime(),
			Paused:  albumQueueState(path) == "paused",
		}

		tracks, ok, err := loadCachedTracks(cachePathFor(path))
		if err != nil {
			return nil, err
		}
		status.Resolved, status.Total, status.tracks = ok, len(tracks), tracks

		for _, t := range tracks {
			logEntry, found := logEntries[t.URL]
			switch {
			case !found:
				status.Untried++
			case logEntry.Status == downloadlog.StatusFailed:
				status.Failed++
			case logEntry.Status == downloadlog.StatusUnavailable:
				status.Unavailable++
			case logEntry.Status == downloadlog.StatusSuccess:
				status.Succeeded++
				if _, statErr := os.Stat(filepath.Join(outputDir, logEntry.File)); statErr != nil {
					status.Missing++
				}
			}
		}

		statuses = append(statuses, status)
	}

	sort.Slice(statuses, func(i, j int) bool { return statuses[i].AddedAt.After(statuses[j].AddedAt) })
	return statuses, nil
}

// TrackStatus is one resolved track's status, cross-referenced against
// the download log — the shape "list" and the GUI actually render: one
// row per track, not one aggregate row per playlist.
type TrackStatus struct {
	Playlist  string `json:"playlist"`
	TrackNo   int    `json:"track_no"` // 1-based position within Playlist
	URL       string `json:"url"`
	TrackName string `json:"trackname"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	// Status is one of: succeeded, failed, unavailable (YouTube no longer
	// serves it), untried, missing (succeeded,
	// but the file's gone from --output since).
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// Paused is whether this track's album is paused (see "hoerbox
	// pause") — orthogonal to Status, which is about the track itself.
	Paused bool `json:"paused,omitempty"`
}

// scanTracks flattens scanInputDir's per-playlist view into one row per
// resolved track, playlist-grouped in the same newest-added-first order.
func scanTracks(inputDir, outputDir string) ([]TrackStatus, error) {
	playlists, err := scanInputDir(inputDir, outputDir)
	if err != nil {
		return nil, err
	}

	logEntries, err := downloadlog.Open(outputDir).All()
	if err != nil {
		return nil, fmt.Errorf("reading download log: %w", err)
	}

	var tracks []TrackStatus
	for _, p := range playlists {
		for i, t := range p.tracks {
			ts := TrackStatus{Playlist: p.Name, TrackNo: i + 1, URL: t.URL, TrackName: t.TrackName, Artist: t.Artist, Album: t.Album, Paused: p.Paused}

			logEntry, found := logEntries[t.URL]
			switch {
			case !found:
				ts.Status = "untried"
			case logEntry.Status == downloadlog.StatusFailed:
				ts.Status, ts.Error = "failed", logEntry.Error
			case logEntry.Status == downloadlog.StatusUnavailable:
				ts.Status, ts.Error = "unavailable", logEntry.Error
			case logEntry.Status == downloadlog.StatusSuccess:
				ts.Status = "succeeded"
				if _, statErr := os.Stat(filepath.Join(outputDir, logEntry.File)); statErr != nil {
					ts.Status = "missing"
				}
			}
			tracks = append(tracks, ts)
		}
	}
	return tracks, nil
}
