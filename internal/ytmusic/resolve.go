// package: ytmusic / integration
// type:    logic
// job:     resolves YouTube Music URLs to track lists and streams a track's audio to raw PCM
// limits:  shells out to yt-dlp+ffmpeg rather than speaking any API directly; no local caching
package ytmusic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/flocko-motion/hoerbox/internal/config"
)

// Track is a single resolved track: hoerbox's whitelist format. A whitelist
// file is just a JSON array of these (see the `resolve`/`whitelist` command
// docs for why URL-level playlist references always flatten to this).
type Track struct {
	URL       string `json:"url"`
	TrackName string `json:"trackname"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	// LengthMs is the track duration in milliseconds, 0 if unknown.
	LengthMs int `json:"length"`
}

// ytdlpEntry mirrors the fields we read out of yt-dlp's --flat-playlist -J
// output, both for a playlist's entries and for a bare single-video result
// (which shares the same field names at the top level).
type ytdlpEntry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Channel  string  `json:"channel"`
	Uploader string  `json:"uploader"`
}

type ytdlpResult struct {
	ytdlpEntry
	Type    string       `json:"_type"`
	Entries []ytdlpEntry `json:"entries"`
}

// Resolve resolves a YouTube/YouTube Music URL — a single video or a
// playlist/album — to its flat track list, via `yt-dlp --flat-playlist`.
// Flat-playlist listing works for public content without authentication;
// see cookieArgs for how to reach account-gated content.
func Resolve(ctx context.Context, log *logrus.Entry, cfg *config.Config, rawURL string) ([]Track, error) {
	args := append([]string{"--flat-playlist", "-J"}, cookieArgs(log, cfg)...)
	args = append(args, rawURL)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var result ytdlpResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parsing yt-dlp output: %w", err)
	}

	if result.Type == "playlist" || len(result.Entries) > 0 {
		tracks := make([]Track, 0, len(result.Entries))
		for _, e := range result.Entries {
			tracks = append(tracks, toTrack(e, result.Title))
		}
		return tracks, nil
	}

	return []Track{toTrack(result.ytdlpEntry, "")}, nil
}

// toTrack converts one yt-dlp entry to hoerbox's Track shape. album is the
// enclosing playlist's title when there is one — flat-playlist entries
// don't carry a per-track album field of their own, and for YouTube
// Music's auto-generated "album" playlists (the OLAK5uy_... ID prefix) the
// playlist title already is the album name.
func toTrack(e ytdlpEntry, album string) Track {
	artist := e.Channel
	if artist == "" {
		artist = e.Uploader
	}
	artist = strings.TrimSuffix(artist, " - Topic")

	return Track{
		URL:       "https://music.youtube.com/watch?v=" + e.ID,
		TrackName: e.Title,
		Artist:    artist,
		Album:     album,
		LengthMs:  int(e.Duration * 1000),
	}
}
