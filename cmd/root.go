// package: cmd / cli
// type:    entrypoint
// job:     wires up the hoerbox cobra CLI: shared config/log setup plus each subcommand
// limits:  business logic lives in internal/*; commands here stay thin wiring
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/config"
)

var verbose bool

// cfg and log are populated in the root command's PersistentPreRunE, so
// every subcommand can rely on them being ready.
var (
	cfg *config.Config
	log *logrus.Entry
)

var rootCmd = &cobra.Command{
	Use:   "hoerbox",
	Short: "hoerbox is a whitelisted YouTube Music speaker client for a Raspberry Pi",
	Long: `hoerbox plays a curated set of YouTube Music tracks/playlists in response to
physical triggers (buttons, RFID tags) and hands off raw PCM audio to a
separate sound-output project. Playback goes through yt-dlp + ffmpeg, not a
Spotify Connect-style protocol client (see README for why).

Subcommands are organized as debugging/development steps first (resolve,
stream, ...) and will grow into a "serve" command that runs hoerbox as the
actual service on the Pi.`,
	SilenceUsage: true,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if err := checkDependencies(); err != nil {
			return err
		}

		cfg = config.New()

		if err := cfg.EnsureDataDir(); err != nil {
			return err
		}

		logger := logrus.StandardLogger()
		logger.SetFormatter(&logrus.TextFormatter{})
		level, err := logrus.ParseLevel(cfg.LogLevel)
		if err != nil {
			level = logrus.InfoLevel
		}
		if verbose {
			level = logrus.DebugLevel
		}
		logger.SetLevel(level)
		log = logrus.NewEntry(logger)

		return nil
	},
}

// Execute runs the root command, canceling its context on SIGINT/SIGTERM
// so exec.CommandContext subprocesses die with it too.
func Execute() {
	augmentPATH()
	chdirForAppBundle()
	watchParentPipe()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if isGUIInvocation() {
			// Finder gives a launched app no visible stderr — without
			// this, a startup failure is silent, just a vanished icon.
			showFatalErrorDialog(err)
		}
		os.Exit(1)
	}
}

// isGUIInvocation reports whether this run would have opened the GUI —
// the only path where stderr isn't visible to whoever launched it.
func isGUIInvocation() bool {
	return len(os.Args) == 1 || (len(os.Args) >= 2 && os.Args[1] == "gui")
}

// augmentPATH adds Homebrew's prefixes to PATH — Finder launches an app
// with none of the shell profile that would normally put them there.
func augmentPATH() {
	extra := []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin"}
	path := os.Getenv("PATH")
	for _, dir := range extra {
		if !strings.Contains(path, dir) {
			path += string(os.PathListSeparator) + dir
		}
	}
	_ = os.Setenv("PATH", path)
}

// parentWatchFDEnv names the env var telling a self-re-exec'd "stream"
// child which inherited fd to watch — see watchParentPipe.
const parentWatchFDEnv = "HOERBOX_PARENT_WATCH_FD"

// watchParentPipe, if parentWatchFDEnv is set, blocks on that fd and
// signals itself the moment it closes — which the OS does the instant the
// parent is gone for any reason, cooperative signal or not.
func watchParentPipe() {
	fdStr := os.Getenv(parentWatchFDEnv)
	if fdStr == "" {
		return
	}
	fd, err := strconv.Atoi(fdStr)
	if err != nil {
		return
	}

	pipe := os.NewFile(uintptr(fd), "parent-watch")
	if pipe == nil {
		return
	}
	go func() {
		_, _ = pipe.Read(make([]byte, 1)) // unblocks once the parent's write end closes
		if self, err := os.FindProcess(os.Getpid()); err == nil {
			_ = self.Signal(os.Interrupt)
		}
	}()
}

// bundledAppProjectDir is the fixed project dir a packaged .app uses,
// independent of where the .app itself is installed — see chdirForAppBundle.
func bundledAppProjectDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Music", "hoerbox")
}

// chdirForAppBundle switches to bundledAppProjectDir() when running inside
// a .app bundle (Finder gives it $HOME as CWD, not anywhere useful). A
// terminal-launched binary is untouched, keeping its real CWD.
func chdirForAppBundle() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	dir := filepath.Dir(exe)
	if filepath.Base(dir) != "MacOS" || filepath.Base(filepath.Dir(dir)) != "Contents" {
		return // not inside a .app bundle
	}

	project := bundledAppProjectDir()
	if project == "" {
		return
	}
	_ = os.MkdirAll(project, 0o755)
	_ = os.Chdir(project)
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
}
