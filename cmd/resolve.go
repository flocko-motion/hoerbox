// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox resolve` — re-resolves every ./in/*.url playlist, refreshing their cache
// limits:  thin wiring; resolution logic lives in internal/ytmusic
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var (
	resolveInput   string
	resolveBrowser string
)

var resolveCmd = &cobra.Command{
	Use:   "resolve",
	Short: "Re-resolve every ./in/*.url playlist, refreshing their cached resolution",
	Long: `Force-refetches every playlist/album/track listed in --input (default
./in/) from YouTube and overwrites each one's cached "*.url.resolved.json"
(see "stream" for what that cache is and why re-running things doesn't
normally hit YouTube again).

Run this occasionally so a playlist's track list doesn't silently drift
out of date — a track renamed or removed upstream won't be noticed
otherwise, since every other command trusts the cache once it exists.

Prints the combined, refreshed track list as JSON: url/trackname/artist/
album/length tuples — hoerbox's whitelist format. Playlists/albums may be
referenced in the .url files for convenience, but hoerbox always plays
and enforces things at the individual-track level, which is exactly the
shape this prints.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if resolveBrowser != "" {
			cfg.YouTube.CookiesFromBrowser = resolveBrowser
		}

		tracks, err := resolveInputDir(ctx, resolveInput, true)
		if err != nil {
			return err
		}

		out, err := json.MarshalIndent(tracks, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling result: %w", err)
		}

		log.Infof("resolved %d tracks", len(tracks))
		fmt.Println(string(out))
		return nil
	},
}

func init() {
	resolveCmd.Flags().StringVar(&resolveInput, "input", "./in/", "directory of *.url files to resolve")
	resolveCmd.Flags().StringVar(&resolveBrowser, "browser", "", "read cookies from this local browser instead of the default (vivaldi)")
	rootCmd.AddCommand(resolveCmd)
}
