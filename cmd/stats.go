// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox stats` — download-log breakdown for a URL (or ./in/*.url) without downloading
// limits:  thin wiring; the log itself lives in internal/downloadlog
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

var (
	statsOutput  string
	statsInput   string
	statsRefresh bool
)

var statsCmd = &cobra.Command{
	Use:   "stats [youtube-music-url]",
	Short: "Show download progress for a URL (or ./in/*.url) against --output's download log",
	Long: `Resolves the URL — or, with no argument, every ./in/*.url file, same as
"stream" — then cross-references every track against --output's
download-log.json: how many are already downloaded, how many failed (with
their latest error), and how many haven't been attempted at all.
Downloads nothing — this only reads the log, freshly, every time.

Resolution itself is cached (see "resolve"), so running this repeatedly to
check on progress doesn't refetch the playlist from YouTube every time.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		var tracks []ytmusic.Track
		var err error
		if len(args) == 1 {
			tracks, err = resolveCached(ctx, urlCachePath(args[0]), args[0], statsRefresh)
		} else {
			tracks, err = resolveInputDir(ctx, statsInput, statsRefresh)
		}
		if err != nil {
			return err
		}

		mlog := downloadlog.Open(statsOutput)

		var succeeded, failed, untried int
		for _, t := range tracks {
			e, ok, err := mlog.Get(t.URL)
			if err != nil {
				return fmt.Errorf("checking download log: %w", err)
			}
			switch {
			case !ok:
				untried++
				printLine("  ○ untried: %s — %s", t.TrackName, t.Artist)
			case e.Status == downloadlog.StatusSuccess:
				succeeded++
			default:
				failed++
				printLine("  ✗ failed: %s — %s: %s", t.TrackName, t.Artist, e.Error)
			}
		}

		printLine("%d tracks total: %d succeeded, %d failed, %d untried", len(tracks), succeeded, failed, untried)
		return nil
	},
}

func init() {
	statsCmd.Flags().StringVar(&statsOutput, "output", "./out/", "directory containing the download log to check against")
	statsCmd.Flags().StringVar(&statsInput, "input", "./in/", "directory of *.url files to resolve when no URL argument is given")
	statsCmd.Flags().BoolVar(&statsRefresh, "refresh", false, "re-fetch URL resolution instead of using a cached one")
	rootCmd.AddCommand(statsCmd)
}
