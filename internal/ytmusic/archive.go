// package: ytmusic / integration
// type:    logic
// job:     archives one track as YouTube's own Opus stream in a tagged .ogg, without re-encoding
// limits:  a track with no Opus stream is transcoded to Opus once; no retry on subprocess failure
package ytmusic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/flocko-motion/hoerbox/internal/config"
)

// ArchiveFormatOgg and ArchiveFormatWAV are config.Audio.ArchiveFormat's
// values: what a track's file in the output directory holds.
const (
	// ArchiveFormatOgg is YouTube's own Opus stream, remuxed into Ogg
	// untouched — the hörbert desktop software takes .ogg (not .opus).
	ArchiveFormatOgg = "ogg"
	// ArchiveFormatWAV is decoded PCM: about 5x the size, no tags.
	ArchiveFormatWAV = "wav"
)

// fallbackOpusBitrate is used only when YouTube has no Opus stream for a
// track and it has to be transcoded; speech needs far less.
const fallbackOpusBitrate = "128k"

// ArchiveTags is the metadata written into an archived file.
type ArchiveTags struct {
	Title, Artist, Album string
	TrackNo              int
}

// ArchiveTrack downloads url's best Opus audio and writes it to
// outputPath (an .ogg) with tags, without re-encoding. The file only
// appears at outputPath once complete.
func ArchiveTrack(ctx context.Context, log *logrus.Entry, cfg *config.Config, url, outputPath string, tags ArchiveTags, onProgress ProgressFunc) error {
	tmpRoot := filepath.Join(cfg.DataDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(tmpRoot, "download-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	source, err := fetchAudio(ctx, log, cfg, url, tmpDir, onProgress)
	if err != nil {
		return err
	}

	codec, err := audioCodec(ctx, source)
	if err != nil {
		return err
	}
	codecArgs := []string{"-c:a", "copy"}
	if codec != "opus" {
		log.Infof("no Opus stream for this track (got %s): transcoding to Opus once", codec)
		codecArgs = []string{"-c:a", "libopus", "-b:a", fallbackOpusBitrate}
	}

	partPath := outputPath + ".part"
	ffArgs := []string{"-hide_banner", "-loglevel", "error", "-i", source, "-map", "0:a:0", "-map_metadata", "-1"}
	ffArgs = append(ffArgs, codecArgs...)
	ffArgs = append(ffArgs, tagArgs(tags)...)
	ffArgs = append(ffArgs, "-f", "ogg", "-y", partPath)

	ffCmd := exec.CommandContext(ctx, "ffmpeg", ffArgs...)
	ffStderr := newTailLogger(log.WithField("proc", "ffmpeg"))
	ffCmd.Stderr = ffStderr
	if err := ffCmd.Run(); err != nil {
		last, _ := ffStderr.tail()
		_ = os.Remove(partPath)
		return fmt.Errorf("ffmpeg: %w: %s", err, last)
	}
	_, _ = ffStderr.tail()

	if err := os.Rename(partPath, outputPath); err != nil {
		_ = os.Remove(partPath)
		return fmt.Errorf("finishing %s: %w", outputPath, err)
	}
	return nil
}

// fetchAudio has yt-dlp download url's best audio — Opus preferred — into
// dir as-is, and returns the file's path.
func fetchAudio(ctx context.Context, log *logrus.Entry, cfg *config.Config, url, dir string, onProgress ProgressFunc) (string, error) {
	args := []string{"-f", "bestaudio[acodec=opus]/bestaudio", "--no-warnings", "--no-part",
		"-o", filepath.Join(dir, "source.%(ext)s")}
	args = append(args, progressArgs...)
	args = append(args, cookieArgs(log, cfg)...)
	args = append(args, url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	out := newTailLogger(log.WithField("proc", "yt-dlp"))
	out.onProgress = onProgress
	cmd.Stdout, cmd.Stderr = out, out
	runErr := cmd.Run()
	last, all := out.tail()
	if runErr != nil {
		return "", &TrackError{Kind: classify(all), Err: fmt.Errorf("yt-dlp: %w: %s", runErr, last)}
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "source.*"))
	if len(matches) != 1 {
		return "", fmt.Errorf("yt-dlp finished but left %d files instead of one in %s", len(matches), dir)
	}
	return matches[0], nil
}

// audioCodec returns path's first audio stream's codec name, e.g. "opus".
func audioCodec(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", path).Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe %s: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// tagArgs turns tags into ffmpeg -metadata flags; ffmpeg's Ogg muxer
// writes them as the standard Vorbis comments (TITLE, TRACKNUMBER, ...).
func tagArgs(t ArchiveTags) []string {
	var args []string
	add := func(key, value string) {
		if value != "" {
			args = append(args, "-metadata", key+"="+value)
		}
	}
	add("title", t.Title)
	add("artist", t.Artist)
	add("album", t.Album)
	if t.TrackNo > 0 {
		add("track", strconv.Itoa(t.TrackNo))
	}
	return args
}
