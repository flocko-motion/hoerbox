// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox add` — resolves a URL and queues it as a named album folder in ./in/
// limits:  names the file from the first track only; refuses to overwrite an existing one
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

var (
	addInput   string
	addBrowser string
)

var addCmd = &cobra.Command{
	Use:   "add <youtube-music-url>",
	Short: "Resolve a URL and add it to ./in/ as a named album folder",
	Long: `Resolves url, derives a filesystem-safe name from its first track's
artist and album (falling back to the track name if there's no album —
a bare single track has none), and creates a "<name>/" album folder in
--input (default ./in/) holding url in its album.url.

The resolution itself is cached in the same folder (tracks.json) (same mechanism
"resolve"/"stream" use), so "stream"/"stats" won't need to re-fetch it.

Refuses to overwrite an existing album with the same derived name.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if addBrowser != "" {
			cfg.YouTube.CookiesFromBrowser = addBrowser
		}
		rawURL := args[0]

		tracks, err := resolveCached(ctx, urlCachePath(rawURL), rawURL, false)
		if err != nil {
			return err
		}
		if len(tracks) == 0 {
			return fmt.Errorf("%s resolved to zero tracks", rawURL)
		}

		return addURLFile(ctx, addInput, rawURL, tracks)
	},
}

// addURLFile writes rawURL into a new album folder "<name>/" under dir, named
// from tracks[0], and seeds its resolution cache with tracks so nothing
// re-fetches it later.
func addURLFile(_ context.Context, dir, rawURL string, tracks []ytmusic.Track) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating input directory: %w", err)
	}

	path := albumURLPath(dir, addFileNameFor(tracks[0]))
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating album folder: %w", err)
	}
	if err := os.WriteFile(path, []byte(rawURL+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := saveCachedTracks(cachePathFor(path), tracks); err != nil {
		return err
	}

	printLine("added %s (%d tracks) -> %s", rawURL, len(tracks), path)
	return nil
}

// addFileNameFor derives a filesystem-safe "Artist - Album" (or "Artist -
// Trackname" when there's no album) name from a track.
func addFileNameFor(t ytmusic.Track) string {
	parts := make([]string, 0, 2)
	if s := sanitizeFilenameComponent(t.Artist); s != "" {
		parts = append(parts, s)
	}
	rest := t.Album
	if rest == "" {
		rest = t.TrackName
	}
	if s := sanitizeFilenameComponent(rest); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "untitled"
	}
	return strings.Join(parts, " - ")
}

func init() {
	addCmd.Flags().StringVar(&addInput, "input", "./in/", "directory to add the album folder to")
	addCmd.Flags().StringVar(&addBrowser, "browser", "", "read cookies from this local browser instead of the default (vivaldi)")
	rootCmd.AddCommand(addCmd)
}
