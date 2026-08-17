// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox repair` — moves already-downloaded files to match the current output naming scheme
// limits:  only ever touches a file download-log.json already marks a success
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
)

var (
	repairInput  string
	repairOutput string
	repairDryRun bool
)

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Move already-downloaded files to match the current output naming scheme",
	Long: `trackOutputPath is the one source of truth for where a track belongs; a
file downloaded under an older layout or numbering scheme is left
wherever it was originally written — "stream" never re-touches a track
already marked a success.

"repair" recomputes every already-succeeded track's correct path the same
way "stream" would (resolving --input's cache), moves any file that's
drifted from that to where it now belongs, and updates its
download-log.json entry to match. --dry-run prints what would move
without touching anything.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		tracks, err := resolveInputDir(ctx, repairInput, false)
		if err != nil {
			return err
		}

		mlog := downloadlog.Open(repairOutput)
		trackNos := albumTrackNumbers(tracks)

		moved, upToDate, skipped := 0, 0, 0
		for i, t := range tracks {
			entry, ok, err := mlog.Get(t.URL)
			if err != nil {
				return fmt.Errorf("checking download log: %w", err)
			}
			if !ok || entry.Status != downloadlog.StatusSuccess {
				skipped++
				continue
			}

			wantPath := trackOutputPath(trackNos[i], t)
			if entry.File == wantPath {
				upToDate++
				continue
			}

			oldFull := filepath.Join(repairOutput, entry.File)
			newFull := filepath.Join(repairOutput, wantPath)
			if _, err := os.Stat(oldFull); err != nil {
				printLine("! %s: recorded file missing, leaving as-is (%s)", t.TrackName, entry.File)
				skipped++
				continue
			}
			if _, err := os.Stat(newFull); err == nil {
				printLine("! %s: target already exists, skipping (%s)", t.TrackName, wantPath)
				skipped++
				continue
			}

			printLine("%s\n  -> %s", entry.File, wantPath)
			if repairDryRun {
				moved++
				continue
			}

			if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", filepath.Dir(newFull), err)
			}
			if err := os.Rename(oldFull, newFull); err != nil {
				return fmt.Errorf("moving %s: %w", entry.File, err)
			}
			removeIfEmpty(filepath.Dir(oldFull))

			entry.File = wantPath
			if err := mlog.Record(entry); err != nil {
				return fmt.Errorf("updating download log for %s: %w", entry.File, err)
			}
			moved++
		}

		verb := "moved"
		if repairDryRun {
			verb = "would move"
		}
		printLine("done: %d %s, %d already correct, %d skipped", moved, verb, upToDate, skipped)
		return nil
	},
}

// removeIfEmpty best-effort removes dir if a move just left it empty; a
// non-empty dir is left alone.
func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

func init() {
	repairCmd.Flags().StringVar(&repairInput, "input", "./in/", "directory of *.url files to repair")
	repairCmd.Flags().StringVar(&repairOutput, "output", "./out/", "directory containing the files and download log to repair")
	repairCmd.Flags().BoolVar(&repairDryRun, "dry-run", false, "print what would move without touching anything")
	rootCmd.AddCommand(repairCmd)
}
