// package: cmd / cli
// type:    entrypoint
// job:     the desktop GUI — a thin Fyne shell that shells out to hoerbox's own subcommands
// limits:  minimal by design (drag-reorder, per-track actions, etc. are all out of scope for now)
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/spf13/cobra"
)

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Open the desktop GUI (also the default when hoerbox is run with no arguments)",
	Args:  cobra.NoArgs,
	RunE:  runGUI,
}

func init() {
	rootCmd.RunE = runGUI // running `hoerbox` with no subcommand opens the GUI
	rootCmd.AddCommand(guiCmd)
}

// runGUI self-re-execs "hoerbox add/stream/list/clean" rather than calling
// that logic directly, so there's exactly one code path for each command.
func runGUI(_ *cobra.Command, _ []string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating hoerbox's own executable: %w", err)
	}

	g := &guiApp{self: self, sortCol: -1}
	g.build()
	g.win.ShowAndRun()
	return nil
}

// guiApp holds everything the GUI needs; widget updates must run on
// Fyne's goroutine (fyne.Do) — most of this is driven by background goroutines.
type guiApp struct {
	self string // path to hoerbox's own binary, for self-re-exec

	app           fyne.App // for Preferences() — see cmd/gui_browser.go
	win           fyne.Window
	table         *widget.Table
	statusLabel   *widget.Label
	startStopBtn  *widget.Button
	terminal      *widget.Entry
	terminalPanel *container.Scroll

	tracks []TrackStatus // last "list --json" result, playlist-grouped, newest-added first

	// sortCol is -1 (server order) until a column header is clicked; asc
	// toggles on a repeat click of the same column. See sortTracks.
	sortCol int
	sortAsc bool

	mu      sync.Mutex
	running *exec.Cmd // the in-flight "stream" subprocess, nil if none
}

// appID must match the --app-id "fyne package" is built with (Makefile's
// "bundle" target), or the .app and this code disagree on preferences.
const appID = "net.omnitopos.hoerbox"

func (g *guiApp) build() {
	a := app.NewWithID(appID)
	g.app = a
	a.Settings().SetTheme(mutedTheme{Theme: theme.DefaultTheme()})
	// macOS resets the CWD during window/GL setup, after app.NewWithID.
	// SetOnStarted is Fyne's "actually running now" hook — re-chdir
	// there, and load the list only after, or it'd race the fix.
	a.Lifecycle().SetOnStarted(func() {
		chdirForAppBundle()
		go g.refreshList()
		go g.startStream()
	})
	g.win = a.NewWindow("hoerbox")

	g.statusLabel = widget.NewLabel("idle")
	g.startStopBtn = widget.NewButton("Start", g.toggleStream)
	g.buildTable()

	g.terminal = widget.NewMultiLineEntry()
	g.terminal.Wrapping = fyne.TextWrapWord
	g.terminal.Disable()
	g.terminalPanel = container.NewScroll(g.terminal)

	toolbar := container.NewHBox(
		widget.NewButton("Add", g.showAddDialog),
		g.startStopBtn,
		widget.NewButton("Remove Done", g.removeDone),
		widget.NewButton("Refresh", func() { go g.refreshList() }),
		widget.NewButton("Open Folder", g.openFolder),
		widget.NewButton("Browser", g.showBrowserDialog),
	)

	split := container.NewVSplit(g.table, g.terminalPanel)
	split.Offset = 0.6

	g.win.SetContent(container.NewBorder(toolbar, g.statusLabel, nil, nil, split))
	g.win.Resize(fyne.NewSize(900, 600))
}

// showFatalErrorDialog is Execute's last resort when hoerbox can't reach
// its own window — Finder gives no visible stderr, so without this a
// startup failure is silent. Blocks until dismissed.
func showFatalErrorDialog(err error) {
	a := app.NewWithID(appID)
	w := a.NewWindow("hoerbox — couldn't start")
	w.Resize(fyne.NewSize(480, 160))
	d := dialog.NewError(err, w)
	d.SetOnClosed(func() { a.Quit() })
	w.Show()
	d.Show()
	a.Run()
}

// openFolder opens the current working directory (where in/, out/, and
// hoerbox itself live) in the OS's file manager.
func (g *guiApp) openFolder() {
	dir, err := os.Getwd()
	if err != nil {
		dialog.ShowError(err, g.win)
		return
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		dialog.ShowError(fmt.Errorf("opening %s: %w", dir, err), g.win)
	}
}
