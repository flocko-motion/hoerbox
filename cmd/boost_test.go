package cmd

import (
	"path/filepath"
	"testing"

	"github.com/flocko-motion/hoerbox/internal/config"
	"github.com/flocko-motion/hoerbox/internal/runstate"
)

func TestBoostFollowsItsAlbum(t *testing.T) {
	cfg = config.New()
	cfg.DataDir = t.TempDir()

	track := func(album string) queuedTrack {
		return queuedTrack{Source: filepath.Join("in", album, "album.url")}
	}
	a, b, c := track("A"), track("B"), track("C")

	// Boost "the next album": binds to the album of the next track.
	if err := runstate.SetBoost(cfg.DataDir, runstate.Boost{}); err != nil {
		t.Fatal(err)
	}
	if got := boostFor(a, b); got != "B" {
		t.Fatalf("unbound boost before a B track: got %q, want B", got)
	}
	if bst, _ := runstate.ReadBoost(cfg.DataDir); bst.Album != "B" {
		t.Fatalf("boost not bound to B: %+v", bst)
	}

	// Still on B: boosted.
	if got := boostFor(b, b); got != "B" {
		t.Errorf("B→B: got %q, want B", got)
	}
	// Moving on from B ends it.
	if got := boostFor(b, c); got != "" {
		t.Errorf("B→C: got %q, want no boost", got)
	}
	if _, ok := runstate.ReadBoost(cfg.DataDir); ok {
		t.Error("boost still on after the run moved on from B")
	}

	// A named album not reached yet stays on, but doesn't apply.
	if err := runstate.SetBoost(cfg.DataDir, runstate.Boost{Album: "C"}); err != nil {
		t.Fatal(err)
	}
	if got := boostFor(a, b); got != "" {
		t.Errorf("C boost during A→B: got %q, want none", got)
	}
	if _, ok := runstate.ReadBoost(cfg.DataDir); !ok {
		t.Error("C boost ended before C was reached")
	}
	if got := boostFor(b, c); got != "C" {
		t.Errorf("C boost on B→C: got %q, want C", got)
	}
}
