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
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/spf13/cobra"

	"github.com/flocko-motion/hoerbox/internal/runstate"
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
	startStopBtn  *widget.Button
	terminal      *widget.Entry
	terminalPanel *container.Scroll
	logWin        fyne.Window   // holds terminalPanel; hidden, not closed, when dismissed
	statusLine    *widget.Label // the bottom bar: the log window's last line (see appendTerminal)

	tracks []TrackStatus // last "list --json" result, playlist-grouped, newest-added first

	// The table's visible rows: one per album, plus the tracks of the one
	// unfolded album. See cmd/gui_table.go.
	rows           []tableRow
	expandedAlbum  string
	lastClickAlbum string
	lastClickAt    time.Time

	// sortCol is -1 (server order) until a column header is clicked; asc
	// toggles on a repeat click of the same column. See sortTracks.
	sortCol int
	sortAsc bool

	// The status panel — see cmd/gui_status.go.
	phaseLabel, nowTitle, nowAlbum, nowFooter *widget.Label
	activityLabel                             *widget.Label
	downloadBar, waitBar                      *widget.ProgressBar // see barTint for the colours
	albumProgress, overallBar                 *widget.ProgressBar
	preparingBusy, startingBusy               *widget.ProgressBarInfinite
	activityRow, albumRow, overallRow         *fyne.Container
	lastNowAlbum                              string // see nowAlbumName
	errLabel                                  *widget.Label
	errBanner                                 *fyne.Container
	errDismissedAt                            time.Time
	localErr                                  *runstate.Error // our own "stream" child died before recording why
	lastCounts                                runstate.Counts
	lastRunning                               bool

	boostBtn *widget.Button // see toggleBoost

	// wantStream: start our own "stream" as soon as the one running
	// elsewhere stops by itself. lastStatus: the latest status.json read.
	// Both only touched on Fyne's goroutine.
	wantStream bool
	lastStatus runstate.Status

	// Album controls: act on the album of the last-selected track.
	albumLabel    *widget.Label
	albumControls *fyne.Container // hidden while no album is selected
	pauseBtn      *widget.Button
	cancelBtn     *widget.Button
	selectedAlbum string
	lastChildLine string // the running child's last non-token output line

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
		go g.pollStatus()
		// Only one "stream" at a time: if one's already going (another
		// window, a terminal), follow it rather than start a second —
		// and take over once it stops (see showStatus), which also
		// covers a just-closed window's run still shutting down.
		g.wantStream = true
		if !runstate.IsRunning(cfg.DataDir) {
			g.wantStream = false
			go g.startStream()
		}
	})
	g.win = a.NewWindow("hoerbox")

	g.startStopBtn = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), g.toggleStream)
	g.startStopBtn.Importance = widget.HighImportance
	g.boostBtn = widget.NewButtonWithIcon("Boost", theme.MediaFastForwardIcon(), g.toggleBoost)
	g.buildTable()

	g.terminal = widget.NewMultiLineEntry()
	g.terminal.Wrapping = fyne.TextWrapWord
	g.terminal.Disable()
	g.terminalPanel = container.NewScroll(g.terminal)

	// The raw log lives in its own window, opened by "Logs": the panel
	// and the table say what's going on, the log is for digging in. It
	// keeps collecting while hidden.
	g.logWin = a.NewWindow("hoerbox — log")
	g.logWin.SetContent(g.terminalPanel)
	g.logWin.Resize(fyne.NewSize(800, 500))
	g.logWin.SetCloseIntercept(g.logWin.Hide)
	// Closing the main window quits, log window or not.
	g.win.SetMaster()

	// Run controls left, housekeeping right.
	toolbar := container.NewHBox(
		widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), g.showAddDialog),
		g.startStopBtn,
		g.boostBtn,
		layout.NewSpacer(),
		widget.NewButtonWithIcon("Remove Done", theme.DeleteIcon(), g.removeDone),
		widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() { go g.refreshList() }),
		widget.NewButtonWithIcon("Open Folder", theme.FolderOpenIcon(), g.openFolder),
		widget.NewButtonWithIcon("Browser", theme.SettingsIcon(), g.showBrowserDialog),
	)

	// The bottom bar is the slim log: the newest line, usually enough to
	// see what yt-dlp is up to; "Logs" opens the whole thing.
	g.statusLine = widget.NewLabel("")
	g.statusLine.Importance = widget.LowImportance
	g.statusLine.Truncation = fyne.TextTruncateEllipsis
	statusBar := container.NewBorder(widget.NewSeparator(), nil, nil,
		widget.NewButtonWithIcon("Logs", theme.ListIcon(), g.showLogs), g.statusLine)

	top := container.NewVBox(toolbar, g.buildStatusPanel())
	g.win.SetContent(container.NewBorder(top, statusBar, nil, nil, g.table))
	g.win.Resize(fyne.NewSize(900, 600))
}

// showLogs opens (or raises) the log window, scrolled to the newest line.
func (g *guiApp) showLogs() {
	g.logWin.Show()
	g.logWin.RequestFocus()
	g.terminalPanel.ScrollToBottom()
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
