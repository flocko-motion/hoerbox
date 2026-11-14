// package: cmd / cli
// type:    entrypoint
// job:     `hoerbox merge` — concatenates a glob of .wav files into a single .wav
// limits:  assumes every matched file shares the same format (true for anything hoerbox itself wrote)
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var mergeCmd = &cobra.Command{
	Use:   "merge <glob> [outfile]",
	Short: "Concatenate a glob of .wav files into a single .wav",
	Long: `Merges every file matching <glob> (quote it, e.g. "biene kapitel*.wav" —
otherwise your shell expands it before hoerbox ever sees it) into one
continuous .wav, in sorted filename order. That's why "stream"
number-prefixes track filenames: sorted order is playback order.

outfile defaults to the glob's own name with wildcards stripped (e.g.
"biene kapitel*.wav" -> "biene kapitel.wav" next to the matched files) —
pass it explicitly to pick something else.

Merging is lossless and fast (ffmpeg's concat demuxer, stream-copied, no
re-encoding) — it assumes every matched file shares the same audio
format, true for anything "stream" itself wrote, since they all go
through the same ffmpeg settings.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		matches, err := filepath.Glob(args[0])
		if err != nil {
			return fmt.Errorf("invalid glob %q: %w", args[0], err)
		}
		var files []string
		for _, m := range matches {
			if strings.EqualFold(filepath.Ext(m), ".wav") {
				files = append(files, m)
			}
		}
		if len(files) == 0 {
			return fmt.Errorf("no .wav files matched %q", args[0])
		}
		sort.Strings(files)

		output := ""
		if len(args) == 2 {
			output = args[1]
		}
		if output == "" {
			output = defaultMergeOutput(args[0], files)
		}

		listPath, err := writeConcatList(files)
		if err != nil {
			return err
		}
		defer os.Remove(listPath)

		for _, f := range files {
			printLine("  + %s", f)
		}
		printLine("merging %d file(s) -> %s", len(files), output)

		ffArgs := []string{"-hide_banner", "-loglevel", "error",
			"-f", "concat", "-safe", "0", "-i", listPath,
			"-c", "copy", "-y", output}
		var out bytes.Buffer
		ffCmd := exec.CommandContext(ctx, "ffmpeg", ffArgs...)
		ffCmd.Stdout, ffCmd.Stderr = &out, &out
		if err := ffCmd.Run(); err != nil {
			return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(out.String()))
		}

		printLine("done: wrote %s", output)
		return nil
	},
}

// globWildcards matches the glob metacharacters defaultMergeOutput strips
// out of a pattern's base name to turn it into a plain filename.
var globWildcards = regexp.MustCompile(`[*?\[\]]+`)

// defaultMergeOutput derives an outfile from pattern's own basename with
// its wildcards removed (e.g. "biene kapitel*.wav" -> "biene
// kapitel.wav"), in pattern's own directory. Falls back to "merged.wav"
// if that would leave nothing recognizable, and to a "-merged" suffix if
// it would otherwise collide with one of the files being merged.
func defaultMergeOutput(pattern string, files []string) string {
	dir := filepath.Dir(pattern)
	name := strings.TrimSpace(globWildcards.ReplaceAllString(filepath.Base(pattern), ""))
	name = strings.Trim(name, "-_ ")
	if name == "" || strings.EqualFold(name, ".wav") {
		name = "merged.wav"
	} else if !strings.EqualFold(filepath.Ext(name), ".wav") {
		name += ".wav"
	}

	out := name
	if dir != "." {
		out = filepath.Join(dir, name)
	}
	if !collides(out, files) {
		return out
	}
	return strings.TrimSuffix(out, filepath.Ext(out)) + "-merged.wav"
}

// collides reports whether candidate (once resolved to an absolute path)
// is one of the files about to be merged — writing the output over one of
// its own inputs mid-merge would corrupt the result.
func collides(candidate string, files []string) bool {
	candAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	for _, f := range files {
		if fAbs, err := filepath.Abs(f); err == nil && fAbs == candAbs {
			return true
		}
	}
	return false
}

// writeConcatList writes files to a temp file in ffmpeg concat-demuxer
// format (one "file '<path>'" per line, single quotes escaped) and
// returns its path. Paths are made absolute so ffmpeg's own relative-path
// resolution (relative to the list file, not the cwd) can't surprise us.
func writeConcatList(files []string) (string, error) {
	f, err := os.CreateTemp("", "hoerbox-merge-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating concat list: %w", err)
	}
	defer f.Close()

	for _, path := range files {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", path, err)
		}
		escaped := strings.ReplaceAll(abs, `'`, `'\''`)
		if _, err := fmt.Fprintf(f, "file '%s'\n", escaped); err != nil {
			return "", fmt.Errorf("writing concat list: %w", err)
		}
	}
	return f.Name(), nil
}

func init() {
	rootCmd.AddCommand(mergeCmd)
}
