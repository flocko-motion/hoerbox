// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox boost` — shorten the pause between downloads until an album is done
// limits:  one album at a time; a running "stream" picks it up within a second
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/runstate"
)

// boostFraction replaces naturalizeFraction while the boosted album
// downloads: a much shorter pause, but still a pause.
const boostFraction = 0.1

var boostCmd = &cobra.Command{
	Use:   "boost [album|off]",
	Short: "Shorten the pause between downloads until the current album is done",
	Long: fmt.Sprintf(`Normally "stream" waits %.0f%% of each track's length before starting the
next download, so it doesn't look like a scraper. A boost cuts that to
%.0f%% for one album, for when it's needed now, and ends by itself once
"stream" moves on to a different album (or the run finishes).
"hoerbox boost off" ends it early.

With no argument, the album downloading right now is boosted — or, if
nothing is downloading at this moment, the next album that does. Name an
album (as "hoerbox list" shows it, or any unambiguous part) to boost
that one when "stream" gets to it; to get there sooner, pause the albums
queued ahead of it.

A running "stream" picks a boost up within a second, even mid-wait.`, naturalizeFraction*100, boostFraction*100),
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if len(args) == 1 && args[0] == "off" {
			if err := runstate.ClearBoost(cfg.DataDir); err != nil {
				return err
			}
			printLine("boost off")
			return nil
		}

		var album string
		if len(args) == 1 {
			path, err := findAlbum(queueInput, args[0])
			if err != nil {
				return err
			}
			album = albumName(path)
		} else if s, ok, err := runstate.Read(cfg.DataDir); err == nil && ok && s.Running && s.Track != nil {
			album = s.Track.Playlist
		}

		if err := runstate.SetBoost(cfg.DataDir, runstate.Boost{Album: album}); err != nil {
			return err
		}
		if album == "" {
			printLine("boost on for the next album downloaded")
		} else {
			printLine("boost on until %s is done", album)
		}
		return nil
	},
}

// describeBoost is b as a human phrase, for "status".
func describeBoost(b runstate.Boost) string {
	if b.Album == "" {
		return "boost on for the next album downloaded"
	}
	return "boost on until " + b.Album + " is done"
}

func init() {
	boostCmd.Flags().StringVar(&queueInput, "input", "./in/", "directory of album folders")
	rootCmd.AddCommand(boostCmd)
}
