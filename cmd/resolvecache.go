// package: cmd / cli
// type:    logic
// job:     caches URL resolutions so re-running a command doesn't refetch them from YouTube
// limits:  caches forever until deleted or --refresh is passed; no TTL, no change detection
package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

// cachePathFor is the cache location for a .url file's own resolution:
// co-located and named after it, so it's discoverable by just looking at
// the directory, and deletable by name to force one playlist to refresh.
func cachePathFor(urlFilePath string) string {
	return urlFilePath + ".resolved.json"
}

// urlCachePath is the cache location for a raw URL with no file of its
// own to sit next to (e.g. one passed directly on the command line):
// hash-keyed, under the data dir.
func urlCachePath(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return filepath.Join(cfg.DataDir, "resolve-cache", hex.EncodeToString(sum[:])+".json")
}

// resolveCached is the one path everything here resolves a URL through:
// a cached lazy fallback, so the same URL is only ever fetched from
// YouTube once (until refresh=true, or the cache file is deleted by
// hand).
func resolveCached(ctx context.Context, cachePath, rawURL string, refresh bool) ([]ytmusic.Track, error) {
	if !refresh {
		cached, ok, err := loadCachedTracks(cachePath)
		if err != nil {
			return nil, err
		}
		if ok {
			printLine("using cached resolution for %s (delete %s, or pass --refresh, to re-fetch)", rawURL, cachePath)
			return cached, nil
		}
	}

	printLine("resolving: %s", rawURL)
	tracks, err := ytmusic.Resolve(ctx, log, cfg, rawURL)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", rawURL, err)
	}
	if err := saveCachedTracks(cachePath, tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

func loadCachedTracks(cachePath string) ([]ytmusic.Track, bool, error) {
	content, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading %s: %w", cachePath, err)
	}

	var tracks []ytmusic.Track
	if err := json.Unmarshal(content, &tracks); err != nil {
		return nil, false, fmt.Errorf("parsing %s: %w", cachePath, err)
	}
	return tracks, true, nil
}

func saveCachedTracks(cachePath string, tracks []ytmusic.Track) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	content, err := json.MarshalIndent(tracks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling resolved tracks: %w", err)
	}
	if err := os.WriteFile(cachePath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", cachePath, err)
	}
	return nil
}
