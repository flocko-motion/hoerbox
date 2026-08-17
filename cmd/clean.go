// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox clean` — removes fully-downloaded playlists from ./in/ and the download log
// limits:  only ever removes a playlist that's 100% succeeded with every file present
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
)

var (
	cleanInput  string
	cleanOutput string
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove fully-downloaded playlists from ./in/ and the download log",
	Long: `For every playlist in --input whose tracks all succeeded and still have
their file (see "list" for that breakdown — "clean" only ever touches a
playlist that's fully Done, never one with failures/missing files),
removes its ".url" file, its cached ".url.resolved.json", and its
tracks' entries from --output's download-log.json.

This is the "I'm done with this one" sweep: once a playlist has been
fully fetched, there's nothing left in ./in/ to act on, so it's cleared
out of the queue rather than sitting there forever.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		statuses, err := scanInputDir(cleanInput, cleanOutput)
		if err != nil {
			return err
		}

		mlog := downloadlog.Open(cleanOutput)
		removed := 0
		for _, s := range statuses {
			if !s.Done() {
				continue
			}

			urls := make([]string, len(s.tracks))
			for i, t := range s.tracks {
				urls[i] = t.URL
			}
			if err := mlog.Delete(urls); err != nil {
				return fmt.Errorf("removing %s's download-log entries: %w", s.Name, err)
			}

			if err := os.Remove(cachePathFor(s.Path)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing %s: %w", cachePathFor(s.Path), err)
			}
			if err := os.Remove(s.Path); err != nil {
				return fmt.Errorf("removing %s: %w", s.Path, err)
			}

			printLine("removed %s (%d tracks, all done)", s.Name, s.Total)
			removed++
		}

		printLine("done: %d playlist(s) removed", removed)
		return nil
	},
}

func init() {
	cleanCmd.Flags().StringVar(&cleanInput, "input", "./in/", "directory of *.url files to clean")
	cleanCmd.Flags().StringVar(&cleanOutput, "output", "./out/", "directory containing the download log to update")
	rootCmd.AddCommand(cleanCmd)
}
