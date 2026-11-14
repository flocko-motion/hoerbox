// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox pause/resume/cancel` — album-level queue control over ./in/<album>/
// limits:  state is plain files in --input, so a running "stream" picks changes up by polling
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// An album in the queue is one ./in/<name>/ folder (see cmd/albumdir.go).
// Pausing it drops a "paused" marker file in that folder — plain files
// rather than a database, so a running "stream" sees the change by just
// looking, and a parent can pause/resume one by hand in Finder too.

func pausedMarkerFor(urlFilePath string) string {
	return albumSibling(urlFilePath, albumPausedFile)
}

// albumQueueState reports whether a track from the album at urlFilePath
// should still be downloaded: "" if so, else "paused" or "cancelled".
// An empty urlFilePath (a URL given straight on the command line, with
// no album file) is always queued.
func albumQueueState(urlFilePath string) string {
	if urlFilePath == "" {
		return ""
	}
	if _, err := os.Stat(urlFilePath); errors.Is(err, os.ErrNotExist) {
		return "cancelled"
	}
	if _, err := os.Stat(pausedMarkerFor(urlFilePath)); err == nil {
		return "paused"
	}
	return ""
}

// findAlbum resolves a user-given album — its name (the folder name, as
// "hoerbox list" shows it), a path to its folder, or an unambiguous
// case-insensitive part of the name — to its url file's path.
func findAlbum(inputDir, query string) (string, error) {
	paths, err := albumURLFiles(inputDir)
	if err != nil {
		return "", err
	}

	want := filepath.Base(strings.TrimSuffix(strings.TrimSuffix(query, "/"), "/"+albumURLFile))
	want = strings.TrimSuffix(want, ".url")
	var matches []string
	var matchPath string
	for _, p := range paths {
		name := albumName(p)
		if name == want {
			return p, nil
		}
		if strings.Contains(strings.ToLower(name), strings.ToLower(want)) {
			matches, matchPath = append(matches, name), p
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no album matching %q in %s (see \"hoerbox list\")", query, inputDir)
	case 1:
		return matchPath, nil
	default:
		return "", fmt.Errorf("%q matches %d albums, be more specific:\n  %s", query, len(matches), strings.Join(matches, "\n  "))
	}
}

var queueInput string

var pauseCmd = &cobra.Command{
	Use:   "pause <album>",
	Short: "Pause an album: \"stream\" skips its tracks until it's resumed",
	Long: `Pauses an album in --input (default ./in/): "stream" skips its tracks
until "hoerbox resume" un-pauses it. A running "stream" notices within a
second — if it's downloading one of this album's tracks right then, it
abandons that download (the track is fetched again on resume).

<album> is the album folder's name (as "hoerbox list" shows it), or any
unambiguous part of it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		path, err := findAlbum(queueInput, args[0])
		if err != nil {
			return err
		}
		if err := os.WriteFile(pausedMarkerFor(path), nil, 0o644); err != nil {
			return fmt.Errorf("pausing: %w", err)
		}
		printLine("paused %s", albumName(path))
		return nil
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume <album>",
	Short: "Resume a paused album",
	Long: `Un-pauses an album paused with "hoerbox pause". Its tracks are picked
up by the next "stream" run — a run already in progress started without
them and won't go back for them.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		path, err := findAlbum(queueInput, args[0])
		if err != nil {
			return err
		}
		if err := os.Remove(pausedMarkerFor(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("resuming: %w", err)
		}
		printLine("resumed %s", albumName(path))
		return nil
	},
}

var cancelCmd = &cobra.Command{
	Use:   "cancel <album>",
	Short: "Cancel an album: remove it from the queue",
	Long: `Removes an album's folder from --input (default ./in/): its album.url,
its cached tracks.json, and its pause marker if any. A running "stream" stops
downloading it within a second.

Tracks already downloaded stay in --output, and stay recorded in the
download log — adding the same album again later won't re-fetch them.`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		path, err := findAlbum(queueInput, args[0])
		if err != nil {
			return err
		}
		if err := removeAlbumFiles(path); err != nil {
			return err
		}
		printLine("cancelled %s (already-downloaded tracks were kept)", albumName(path))
		return nil
	},
}

func init() {
	for _, c := range []*cobra.Command{pauseCmd, resumeCmd, cancelCmd} {
		c.Flags().StringVar(&queueInput, "input", "./in/", "directory of album folders")
		rootCmd.AddCommand(c)
	}
}
