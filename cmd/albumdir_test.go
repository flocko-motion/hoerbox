package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flocko-motion/hoerbox/internal/config"
)

func TestMigrateFlatInputMovesAlbumsIntoFolders(t *testing.T) {
	cfg = config.New()
	cfg.DataDir = t.TempDir()
	in := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(in, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Bibi - Folge 164.url", "https://music.youtube.com/playlist?list=A\n")
	write("Bibi - Folge 164.url.resolved.json", "[]")
	write("Sandmann - 004.url", "https://music.youtube.com/playlist?list=B\n")
	write("Sandmann - 004.url.paused", "")
	write("notes.txt", "not ours")

	paths, err := albumURLFiles(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || albumName(paths[0]) != "Bibi - Folge 164" || albumName(paths[1]) != "Sandmann - 004" {
		t.Fatalf("albums = %v", paths)
	}
	for _, p := range []string{
		"Bibi - Folge 164/album.url", "Bibi - Folge 164/tracks.json",
		"Sandmann - 004/album.url", "Sandmann - 004/paused", "notes.txt",
	} {
		if _, err := os.Stat(filepath.Join(in, p)); err != nil {
			t.Errorf("want %s after migration: %v", p, err)
		}
	}
	if got := albumQueueState(paths[1]); got != "paused" {
		t.Errorf("Sandmann queue state = %q, want paused", got)
	}
	if got := cachePathFor(paths[0]); got != filepath.Join(in, "Bibi - Folge 164", "tracks.json") {
		t.Errorf("cachePathFor = %s", got)
	}

	found, err := findAlbum(in, "Sandmann - 004")
	if err != nil || found != paths[1] {
		t.Fatalf("findAlbum exact = %q, %v", found, err)
	}

	if err := removeAlbumFiles(paths[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(in, "Sandmann - 004")); !os.IsNotExist(err) {
		t.Errorf("cancelled album folder should be gone, stat err = %v", err)
	}
}

func TestFlatAlbumHelpersBeforeMigration(t *testing.T) {
	p := "/in/Old Album.url"
	if albumName(p) != "Old Album" || cachePathFor(p) != "/in/Old Album.url.resolved.json" || pausedMarkerFor(p) != "/in/Old Album.url.paused" {
		t.Errorf("flat helpers: %s %s %s", albumName(p), cachePathFor(p), pausedMarkerFor(p))
	}
}
