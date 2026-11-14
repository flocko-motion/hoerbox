// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox status` (what "stream" is doing, last error) and `hoerbox errors` (recent failures)
// limits:  read-only; both only read data/status.json and the download log
package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/downloadlog"
	"github.com/flocko-motion/hoerbox/internal/runstate"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what \"stream\" is doing right now, and the last error",
	Long: `Shows whether a "stream" run is going (only one can run at a time), what
it's working on, its progress, and the last error any run reported —
which is kept after that run ends, so you can still see why it stopped.

"hoerbox errors" lists every track whose latest attempt failed.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		s, ok, err := runstate.Read(cfg.DataDir)
		if err != nil {
			return err
		}
		if statusJSON {
			out, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				return fmt.Errorf("marshalling status: %w", err)
			}
			fmt.Println(string(out))
			return nil
		}
		if !ok {
			if runstate.IsRunning(cfg.DataDir) {
				printLine("running — just started, no status recorded yet")
			} else {
				printLine("not running — no run recorded yet")
			}
			return nil
		}

		for _, line := range describeStatus(s, time.Now()) {
			printLine("%s", line)
		}
		if b, ok := runstate.ReadBoost(cfg.DataDir); ok {
			printLine("  %s — \"hoerbox boost off\" to end it", describeBoost(b))
		}
		if e := s.LastError; e != nil {
			printLine("")
			printLine("last error (%s, %s ago):", e.At.Format("Mon 15:04"), ago(e.At, time.Now()))
			if e.Track != "" {
				printLine("  track: %s", e.Track)
			}
			printLine("  %s", e.Message)
		}
		return nil
	},
}

// describeStatus is s as a few human lines: the run state, then what
// it's on and its progress. Shared by "status" and the GUI.
func describeStatus(s runstate.Status, now time.Time) []string {
	var lines []string
	switch {
	case s.Running:
		origin := "from a terminal"
		if s.StartedBy == "gui" {
			origin = "by the hoerbox window"
		}
		lines = append(lines, fmt.Sprintf("running (pid %d, started %s %s, %s ago)", s.PID, origin, s.StartedAt.Format("15:04"), ago(s.StartedAt, now)))
		lines = append(lines, "  "+s.Message)
	case s.Active():
		// Never got to say it ended: crashed or was killed mid-run.
		lines = append(lines, fmt.Sprintf("not running — the last run (pid %d) died while %s, %s ago", s.PID, s.Phase, ago(s.UpdatedAt, now)))
		lines = append(lines, "  last seen: "+s.Message)
	default:
		lines = append(lines, fmt.Sprintf("not running — last run ended %s ago", ago(s.UpdatedAt, now)))
		lines = append(lines, "  "+s.Message)
	}

	if c := s.Counts; c.Total > 0 {
		lines = append(lines, "  "+describeCounts(c))
	}
	return lines
}

func describeCounts(c runstate.Counts) string {
	untried := c.Total - c.Succeeded - c.Failed - c.Skipped - c.Paused - c.Unavailable
	return fmt.Sprintf("%d tracks: %d downloaded, %d already there, %d failed, %d unavailable, %d paused, %d to go",
		c.Total, c.Succeeded, c.Skipped, c.Failed, c.Unavailable, c.Paused, untried)
}

// ago is d rounded to something readable: "12s", "4m", "3h", "2d".
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

var (
	errorsOutput string
	errorsLimit  int
)

var errorsCmd = &cobra.Command{
	Use:   "errors",
	Short: "List the most recent failed downloads, newest first, with full errors",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		entries, err := downloadlog.Open(errorsOutput).All()
		if err != nil {
			return err
		}

		var failed []downloadlog.Entry
		for _, e := range entries {
			if e.Status != downloadlog.StatusSuccess {
				failed = append(failed, e)
			}
		}
		sort.Slice(failed, func(i, j int) bool { return failed[i].AttemptedAt.After(failed[j].AttemptedAt) })

		if len(failed) == 0 {
			printLine("no failed downloads")
			return nil
		}
		shown := failed
		if errorsLimit > 0 && len(shown) > errorsLimit {
			shown = shown[:errorsLimit]
		}
		for _, e := range shown {
			printLine("%s  %s — %s (%s)", e.AttemptedAt.Format("2006-01-02 15:04"), e.TrackName, e.Artist, e.Album)
			printLine("  %s", e.Error)
		}
		if len(shown) < len(failed) {
			printLine("(%d of %d failed tracks shown — -n 0 for all)", len(shown), len(failed))
		}
		return nil
	},
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "print the raw status as JSON")
	errorsCmd.Flags().StringVar(&errorsOutput, "output", "./out/", "directory containing the download log")
	errorsCmd.Flags().IntVarP(&errorsLimit, "limit", "n", 10, "show at most this many (0: all)")
	rootCmd.AddCommand(statusCmd, errorsCmd)
}
