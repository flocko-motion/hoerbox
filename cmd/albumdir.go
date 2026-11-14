// package: cmd / cli
// type:    logic
// job:     the ./in/ layout — one folder per album — and migrating the old flat *.url layout into it
// limits:  migration only moves files hoerbox itself named; anything else in ./in/ is left alone
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flocko-motion/hoerbox/internal/runstate"
)

// An album in the queue is one folder under ./in/, named after it:
//
//	in/<album>/album.url    the YouTube Music link — its presence is what queues the album
//	in/<album>/tracks.json  the resolved track list (see resolveCached)
//	in/<album>/paused       marker: "stream" skips the album while it exists
//
// Code throughout refers to an album by its album.url path ("urlFilePath").
// Until migrateFlatInput has run, an album may still be in the old flat
// layout — in/<album>.url with <album>.url.resolved.json and
// <album>.url.paused beside it — and every helper here handles both.
const (
	albumURLFile    = "album.url"
	albumTracksFile = "tracks.json"
	albumPausedFile = "paused"
)

// albumURLPath is where album name's album.url lives under inputDir.
func albumURLPath(inputDir, name string) string {
	return filepath.Join(inputDir, name, albumURLFile)
}

func isFolderAlbum(urlFilePath string) bool {
	return filepath.Base(urlFilePath) == albumURLFile
}

// albumName is an album's display name: its folder's name.
func albumName(urlFilePath string) string {
	if isFolderAlbum(urlFilePath) {
		return filepath.Base(filepath.Dir(urlFilePath))
	}
	return strings.TrimSuffix(filepath.Base(urlFilePath), ".url")
}

// albumSibling is the path of an album's file kind (albumTracksFile or
// albumPausedFile), in whichever layout the album is in.
func albumSibling(urlFilePath, kind string) string {
	if isFolderAlbum(urlFilePath) {
		return filepath.Join(filepath.Dir(urlFilePath), kind)
	}
	flat := map[string]string{albumTracksFile: ".resolved.json", albumPausedFile: ".paused"}
	return urlFilePath + flat[kind]
}

// albumURLFiles returns every queued album's url file under inputDir,
// sorted by album name. It first migrates old flat-layout albums into
// folders, unless a "stream" run is going: that run polls its albums'
// paths, and would see moved ones as cancelled (stream migrates itself,
// under its lock).
func albumURLFiles(inputDir string) ([]string, error) {
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating input directory %s: %w", inputDir, err)
	}
	if !runstate.IsRunning(cfg.DataDir) {
		if err := migrateFlatInput(inputDir); err != nil {
			return nil, err
		}
	}

	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return nil, fmt.Errorf("reading input directory %s: %w", inputDir, err)
	}

	var paths []string
	for _, e := range entries {
		switch {
		case e.IsDir():
			path := albumURLPath(inputDir, e.Name())
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		case strings.HasSuffix(e.Name(), ".url"): // not migrated yet
			paths = append(paths, filepath.Join(inputDir, e.Name()))
		}
	}
	sort.Slice(paths, func(i, j int) bool { return albumName(paths[i]) < albumName(paths[j]) })
	return paths, nil
}

// removeAlbumFiles dequeues an album: album.url first — its absence is
// what a running "stream" reads as cancelled, so it sees that at once —
// then the files kept beside it, then the folder itself if nothing else
// is left in it.
func removeAlbumFiles(urlFilePath string) error {
	for _, p := range []string{urlFilePath, albumSibling(urlFilePath, albumTracksFile), albumSibling(urlFilePath, albumPausedFile)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	if !isFolderAlbum(urlFilePath) {
		return nil
	}
	dir := filepath.Dir(urlFilePath)
	_ = os.Remove(filepath.Join(dir, ".DS_Store")) // Finder's, not the user's
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		printLine("  left %s in place: it still holds files hoerbox didn't put there", dir)
	}
	return nil
}

// migrateFlatInput moves the old flat layout — <name>.url with
// <name>.url.resolved.json and <name>.url.paused beside it — into
// <name>/ folders. Idempotent: once moved, there's nothing left to match.
func migrateFlatInput(inputDir string) error {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return fmt.Errorf("reading input directory %s: %w", inputDir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".url") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".url")
		oldURL := filepath.Join(inputDir, e.Name())
		newURL := albumURLPath(inputDir, name)

		if _, err := os.Stat(newURL); err == nil {
			printLine("! not migrating %s: %s already exists", oldURL, newURL)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(newURL), 0o755); err != nil {
			return fmt.Errorf("creating album folder for %s: %w", oldURL, err)
		}

		// album.url last: until it moves, the album still shows up (and
		// downloads) under its old flat name, so a half-done migration
		// loses nothing.
		moves := [][2]string{
			{oldURL + ".resolved.json", filepath.Join(filepath.Dir(newURL), albumTracksFile)},
			{oldURL + ".paused", filepath.Join(filepath.Dir(newURL), albumPausedFile)},
			{oldURL, newURL},
		}
		for _, m := range moves {
			if err := os.Rename(m[0], m[1]); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("migrating %s to %s: %w", m[0], m[1], err)
			}
		}
		printLine("moved album %q into its own folder: %s", name, filepath.Dir(newURL))
	}
	return nil
}
