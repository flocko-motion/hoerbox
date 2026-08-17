// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox list` — every resolved track's status (or JSON, for the GUI)
// limits:  thin wiring; the actual computation is scanTracks in library.go
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	listInput  string
	listOutput string
	listJSON   bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Show every resolved track's status, one row per track",
	Long: `Lists every track resolved from --input's (default ./in/) playlists, one
row per track: which playlist it's from, name/artist, and its status —
succeeded, failed, untried, or missing (succeeded, but the file's gone
from --output since — moved or deleted after the fact). Playlist-grouped,
newest-added playlist first. Reads only cached data, never the network.

--json prints the same data as a JSON array instead of a table; this is
what the GUI polls after each track "stream" finishes, rather than
continuously.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		tracks, err := scanTracks(listInput, listOutput)
		if err != nil {
			return err
		}

		if listJSON {
			out, err := json.MarshalIndent(tracks, "", "  ")
			if err != nil {
				return fmt.Errorf("marshalling result: %w", err)
			}
			fmt.Println(string(out))
			return nil
		}

		for _, t := range tracks {
			line := fmt.Sprintf("[%-9s] %-20s %s — %s", t.Status, t.Playlist, t.TrackName, t.Artist)
			if t.Error != "" {
				line += ": " + t.Error
			}
			printLine("%s", line)
		}
		printLine("%d tracks total", len(tracks))
		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listInput, "input", "./in/", "directory of *.url files to list")
	listCmd.Flags().StringVar(&listOutput, "output", "./out/", "directory containing the download log to check against")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "print as JSON instead of a table")
	rootCmd.AddCommand(listCmd)
}
