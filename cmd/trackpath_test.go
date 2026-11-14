package cmd

import (
	"path/filepath"
	"testing"

	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

func TestTrackOutputPathUsesFormatExtension(t *testing.T) {
	tr := ytmusic.Track{Artist: "Bibi Blocksberg", TrackName: "Titellied", Album: "Folge 164"}
	for ext, want := range map[string]string{
		"ogg":  "Folge 164/002 - Bibi Blocksberg - Titellied.ogg",
		".wav": "Folge 164/002 - Bibi Blocksberg - Titellied.wav", // repair passes filepath.Ext
	} {
		if got := trackOutputPath(2, tr, ext); got != filepath.FromSlash(want) {
			t.Errorf("ext %q: got %q, want %q", ext, got, want)
		}
	}
}
