// package: cmd / cli
// type:    logic
// job:     the GUI's "now" card (progress bars), last-error banner, and album controls (pause/resume/cancel)
// limits:  polls data/status.json twice a second — whichever process is streaming, ours or not
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/flocko-motion/hoerbox/internal/runstate"
)

// buildStatusPanel creates the widgets between the toolbar and the
// table: a "now" card with progress bars, the last-error banner, and the
// selected album's controls (hidden until an album is selected).
func (g *guiApp) buildStatusPanel() fyne.CanvasObject {
	g.phaseLabel = widget.NewLabel("Idle")
	g.phaseLabel.TextStyle = fyne.TextStyle{Bold: true}
	g.nowTitle = widget.NewLabel("")
	g.nowTitle.TextStyle = fyne.TextStyle{Bold: true}
	g.nowTitle.Truncation = fyne.TextTruncateEllipsis
	g.nowAlbum = widget.NewLabel("")
	g.nowAlbum.Importance = widget.LowImportance
	g.nowAlbum.Truncation = fyne.TextTruncateEllipsis
	header := container.NewBorder(nil, nil, g.phaseLabel, nil,
		container.NewGridWithColumns(2, g.nowTitle, g.nowAlbum))

	// One row, one look per state of the run (see showActivity and
	// barTint): preparing, starting a track, downloading, waiting.
	g.downloadBar = widget.NewProgressBar()
	g.waitBar = widget.NewProgressBar()
	g.preparingBusy = widget.NewProgressBarInfinite()
	g.startingBusy = widget.NewProgressBarInfinite()
	for _, o := range []fyne.CanvasObject{g.downloadBar, g.waitBar, g.preparingBusy, g.startingBusy} {
		o.Hide()
	}
	g.activityRow, g.activityLabel = barRow("Track", container.NewStack(
		tinted(g.downloadBar, tintDownloading), tinted(g.waitBar, tintWaiting),
		tinted(g.preparingBusy, tintPreparing), tinted(g.startingBusy, tintStarting)))
	g.albumProgress = widget.NewProgressBar()
	g.albumRow, _ = barRow("Album", tinted(g.albumProgress, tintNeutral))
	g.overallBar = widget.NewProgressBar()
	g.overallRow, _ = barRow("Overall", tinted(g.overallBar, tintNeutral))

	g.nowFooter = widget.NewLabel("")
	g.nowFooter.Importance = widget.LowImportance
	g.nowFooter.Truncation = fyne.TextTruncateEllipsis

	card := widget.NewCard("", "", container.NewVBox(header, g.activityRow, g.albumRow, g.overallRow, g.nowFooter))

	g.errLabel = widget.NewLabel("")
	g.errLabel.Importance = widget.DangerImportance
	g.errLabel.Wrapping = fyne.TextWrapWord
	dismiss := widget.NewButton("Dismiss", func() {
		g.errDismissedAt = time.Now()
		g.errBanner.Hide()
	})
	g.errBanner = container.NewBorder(nil, nil, widget.NewIcon(theme.ErrorIcon()),
		container.NewHBox(widget.NewButton("Logs", g.showLogs), dismiss), g.errLabel)
	g.errBanner.Hide()

	g.albumLabel = widget.NewLabel("")
	g.albumLabel.TextStyle = fyne.TextStyle{Bold: true}
	g.albumLabel.Truncation = fyne.TextTruncateEllipsis
	g.pauseBtn = widget.NewButtonWithIcon("Pause Album", theme.MediaPauseIcon(), g.togglePauseAlbum)
	g.cancelBtn = widget.NewButtonWithIcon("Cancel Album", theme.CancelIcon(), g.confirmCancelAlbum)
	g.cancelBtn.Importance = widget.DangerImportance
	g.albumControls = container.NewBorder(nil, nil, widget.NewIcon(theme.FolderIcon()),
		container.NewHBox(g.pauseBtn, g.cancelBtn), g.albumLabel)
	g.albumControls.Hide()

	return container.NewVBox(card, g.errBanner, g.albumControls)
}

// barRow lays out one labelled progress row of the "now" card, the
// labels all one width so the bars line up.
func barRow(label string, bar fyne.CanvasObject) (*fyne.Container, *widget.Label) {
	l := widget.NewLabel(label)
	l.Importance = widget.LowImportance
	width := widget.NewLabel("Overall_").MinSize().Width
	return container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(width, l.MinSize().Height), l), nil, bar), l
}

// pollStatus re-reads data/status.json until the app quits. It's how
// the panel follows a "stream" whether this window started it or not.
func (g *guiApp) pollStatus() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		s, ok, err := runstate.Read(cfg.DataDir)
		if err != nil {
			continue // mid-replace, or briefly unreadable: next tick
		}
		fyne.Do(func() { g.showStatus(s, ok) })
	}
}

// showStatus renders s (ok false: nothing has ever run) into the panel.
// Runs on Fyne's goroutine.
func (g *guiApp) showStatus(s runstate.Status, ok bool) {
	now := time.Now()
	g.mu.Lock()
	own := g.running != nil
	g.mu.Unlock()

	if own || s.Running {
		setButton(g.startStopBtn, "Stop", theme.MediaStopIcon())
	} else {
		setButton(g.startStopBtn, "Start", theme.MediaPlayIcon())
	}
	g.startStopBtn.Enable()

	g.showNow(s, ok, own, now)

	// A "stream" this window doesn't own only tells us about progress
	// here; refresh the table when its numbers move.
	if !own && ok && (s.Counts != g.lastCounts || s.Running != g.lastRunning) {
		go g.refreshList()
	}
	// The downloading track's row shows its progress: keep it moving.
	if trackProgressChanged(g.lastStatus, s) {
		g.table.Refresh()
	}
	g.lastCounts, g.lastRunning = s.Counts, s.Running
	g.lastStatus = s

	// The run we deferred to at startup is over: if it was stopped
	// (its window closed, or someone stopped it) or died, carry on with
	// our own. One that finished, or failed, is left alone — re-running
	// would only repeat it.
	if g.wantStream && !own && !s.Running && (s.Phase == runstate.PhaseStopped || s.Active()) {
		g.wantStream = false
		go g.startStream()
	}

	g.showLastError(s.LastError)
	g.showBoost()
}

// showNow fills the "now" card from s.
func (g *guiApp) showNow(s runstate.Status, ok, own bool, now time.Time) {
	phase, importance := phaseBadge(s, ok)
	g.phaseLabel.Importance = importance
	setLabel(g.phaseLabel, phase)

	title, album := "", ""
	switch {
	case !ok:
		title = "Nothing has run yet. Press Start."
	case s.Track != nil:
		title, album = s.Track.Name+" — "+s.Track.Artist, s.Track.Playlist
	case s.Running:
		title = strings.TrimSuffix(s.Message, "...")
	default:
		title = s.Message
	}
	setLabel(g.nowTitle, title)
	setLabel(g.nowAlbum, album)

	// Activity row: what the run is doing right now.
	switch {
	case s.Running && s.Track != nil && s.Track.Size == 0:
		g.showActivity("Starting", g.startingBusy)
	case s.Running && s.Track != nil:
		g.showBar(g.downloadBar, downloadFraction(s.Track), downloadText(s.Track))
		g.showActivity("Track", g.downloadBar)
	case s.Running && s.Wait != nil:
		total := s.Wait.Until.Sub(s.Wait.From)
		left := max(s.Wait.Until.Sub(now), 0)
		fraction := 1.0
		if total > 0 {
			fraction = 1 - float64(left)/float64(total)
		}
		g.showBar(g.waitBar, fraction, left.Round(time.Second).String())
		g.showActivity("Next in", g.waitBar)
	case s.Running && (s.Phase == runstate.PhaseResolving || s.Phase == runstate.PhaseStarting):
		g.showActivity("Preparing", g.preparingBusy)
	default:
		g.showActivity("", nil)
		g.activityRow.Hide()
	}

	// Album row: the album downloading now (or waited on), from the list.
	if a := g.nowAlbumName(s); s.Running && a != "" {
		done, total, failed := g.albumTally(a)
		text := fmt.Sprintf("%d of %d tracks", done, total)
		if failed > 0 {
			text += fmt.Sprintf(" · %d failed", failed)
		}
		g.showBar(g.albumProgress, ratio(done, total), text)
		g.albumRow.Show()
	} else {
		g.albumRow.Hide()
	}

	// Overall row: the whole run's progress.
	if c := s.Counts; ok && c.Total > 0 {
		done := c.Succeeded + c.Skipped + c.Unavailable
		text := fmt.Sprintf("%d of %d tracks", done, c.Total)
		for _, n := range []struct {
			count int
			what  string
		}{{c.Failed, "failed"}, {c.Unavailable, "unavailable"}, {c.Paused, "paused"}} {
			if n.count > 0 {
				text += fmt.Sprintf(" · %d %s", n.count, n.what)
			}
		}
		g.showBar(g.overallBar, ratio(done, c.Total), text)
		g.overallRow.Show()
	} else {
		g.overallRow.Hide()
	}

	footer := ""
	switch {
	case !ok:
	case s.Running && own:
		footer = fmt.Sprintf("running for %s, started by this window", ago(s.StartedAt, now))
	case s.Running:
		footer = fmt.Sprintf("running for %s, %s", ago(s.StartedAt, now), foreignOrigin(s))
	case s.Active():
		footer = fmt.Sprintf("the last run stopped responding %s ago", ago(s.UpdatedAt, now))
	default:
		footer = fmt.Sprintf("last run ended %s ago", ago(s.UpdatedAt, now))
	}
	setLabel(g.nowFooter, footer)
}

// phaseBadge is the short, colour-coded word heading the "now" card.
func phaseBadge(s runstate.Status, ok bool) (string, widget.Importance) {
	switch {
	case !ok:
		return "IDLE", widget.LowImportance
	case s.Running && s.Phase == runstate.PhaseDownloading && (s.Track == nil || s.Track.Size == 0):
		return "STARTING", widget.WarningImportance
	case s.Running && s.Phase == runstate.PhaseDownloading:
		return "DOWNLOADING", widget.SuccessImportance
	case s.Running && s.Phase == runstate.PhaseWaiting:
		return "WAITING", widget.DangerImportance
	case s.Running:
		return "PREPARING", widget.MediumImportance
	case s.Phase == runstate.PhaseDone:
		return "DONE", widget.SuccessImportance
	case s.Phase == runstate.PhaseFailed:
		return "FAILED", widget.DangerImportance
	case s.Active():
		return "DIED", widget.DangerImportance
	default:
		return "STOPPED", widget.LowImportance
	}
}

// nowAlbumName is the album the run is busy with: the downloading track's,
// or while waiting, the one the last track came from.
func (g *guiApp) nowAlbumName(s runstate.Status) string {
	if s.Track != nil {
		g.lastNowAlbum = s.Track.Playlist
	}
	return g.lastNowAlbum
}

// albumTally counts an album's tracks in the last list: done (downloaded
// or unavailable), total, and failed.
func (g *guiApp) albumTally(playlist string) (done, total, failed int) {
	for _, t := range g.tracks {
		if t.Playlist != playlist {
			continue
		}
		total++
		switch t.Status {
		case "succeeded", "unavailable":
			done++
		case "failed":
			failed++
		}
	}
	return done, total, failed
}

// showActivity shows the activity row with label and just one of its
// looks (nil: none), keeping only a visible moving bar animating.
func (g *guiApp) showActivity(label string, look fyne.CanvasObject) {
	setLabel(g.activityLabel, label)
	for _, o := range []fyne.CanvasObject{g.downloadBar, g.waitBar, g.preparingBusy, g.startingBusy} {
		if o == look {
			o.Show()
		} else {
			o.Hide()
		}
	}
	for _, busy := range []*widget.ProgressBarInfinite{g.preparingBusy, g.startingBusy} {
		switch {
		case busy == look && !busy.Running():
			busy.Start()
		case busy != look && busy.Running():
			busy.Stop()
		}
	}
	g.activityRow.Show()
}

// showBar sets a progress bar's fill and the text drawn on it.
func (g *guiApp) showBar(bar *widget.ProgressBar, fraction float64, text string) {
	bar.TextFormatter = func() string { return text }
	bar.SetValue(min(max(fraction, 0), 1))
	bar.Show()
}

func downloadFraction(t *runstate.Track) float64 {
	if t.Size <= 0 {
		return 0
	}
	return float64(t.Downloaded) / float64(t.Size)
}

func downloadText(t *runstate.Track) string {
	if t.Size <= 0 {
		return "starting…"
	}
	return fmt.Sprintf("%.0f%% · %.1f of %.1f MB", 100*downloadFraction(t), mb(t.Downloaded), mb(t.Size))
}

func mb(bytes int64) float64 { return float64(bytes) / (1 << 20) }

func ratio(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

// trackProgressChanged reports whether the downloading track, or how
// far along it is, differs between two status reads.
func trackProgressChanged(a, b runstate.Status) bool {
	switch {
	case a.Track == nil && b.Track == nil:
		return false
	case a.Track == nil || b.Track == nil:
		return true
	default:
		return a.Track.URL != b.Track.URL || a.Track.Downloaded != b.Track.Downloaded
	}
}

// setLabel and setButton skip no-op updates, which would otherwise
// redraw twice a second for nothing.
func setLabel(l *widget.Label, text string) {
	if l.Text != text {
		l.SetText(text)
	} else {
		l.Refresh() // Importance may have changed
	}
}

func setButton(b *widget.Button, text string, icon fyne.Resource) {
	if b.Text != text {
		b.SetText(text)
		b.SetIcon(icon)
	}
}

// foreignOrigin says where a run this window didn't start came from.
func foreignOrigin(s runstate.Status) string {
	if s.StartedBy == "gui" {
		return "started by another hoerbox window"
	}
	return "started from a terminal"
}

// confirmStopForeign asks before stopping a run another window or a
// terminal started, then signals it the same graceful way Stop does our own.
func (g *guiApp) confirmStopForeign(s runstate.Status) {
	dialog.ShowConfirm("Stop download",
		fmt.Sprintf("The running download (pid %d) was %s.\n\nStop it?", s.PID, foreignOrigin(s)),
		func(ok bool) {
			if !ok {
				return
			}
			g.wantStream = false // stopping, not handing over: don't restart it here
			p, err := os.FindProcess(s.PID)
			if err == nil {
				err = p.Signal(os.Interrupt)
			}
			if err != nil {
				dialog.ShowError(fmt.Errorf("stopping pid %d: %w", s.PID, err), g.win)
			}
		}, g.win)
}

// showBoost labels the Boost button with the boosted album, if any.
func (g *guiApp) showBoost() {
	text := "Boost Album"
	if b, ok := runstate.ReadBoost(cfg.DataDir); ok {
		text = "Boost on: next album"
		if b.Album != "" {
			text = "Boost on: " + b.Album
		}
		g.boostBtn.Importance = widget.HighImportance
	} else {
		g.boostBtn.Importance = widget.MediumImportance
	}
	if g.boostBtn.Text != text {
		g.boostBtn.SetText(text)
	}
	g.boostBtn.Refresh()
}

// toggleBoost is the Boost button's handler: boosts the album
// downloading now (see "hoerbox boost"), or ends the boost that's on.
func (g *guiApp) toggleBoost() {
	if _, ok := runstate.ReadBoost(cfg.DataDir); !ok {
		go g.runOneShot("boost")
	} else {
		go g.runOneShot("boost", "off")
	}
}

// showLastError shows the newer of e (from status.json) and g.localErr
// (our own "stream" child dying before it could record anything), unless
// it's been dismissed since.
func (g *guiApp) showLastError(e *runstate.Error) {
	if l := g.localErr; l != nil && (e == nil || l.At.After(e.At.Add(2*time.Second))) {
		e = l
	}
	if e == nil || !e.At.After(g.errDismissedAt) {
		g.errBanner.Hide()
		return
	}

	text := fmt.Sprintf("Last error (%s, %s ago): ", e.At.Format("15:04"), ago(e.At, time.Now()))
	if e.Track != "" {
		text += e.Track + ": "
	}
	text += oneLine(e.Message)
	if g.errLabel.Text != text {
		g.errLabel.SetText(text)
	}
	g.errBanner.Show()
}

// selectTrackRow is the table's OnSelected: a click on an album row, or
// on one of its tracks, selects that album for the album controls; a
// double-click also folds/unfolds it (see clickedAlbum).
func (g *guiApp) selectTrackRow(id widget.TableCellID) {
	if id.Row < 0 || id.Row >= len(g.rows) {
		return
	}
	// Fyne won't fire OnSelected again for the already-selected cell, so
	// unselect at once: the album bar shows what's selected, and a
	// second click must get through for double-click to work.
	g.table.Unselect(id)
	playlist := g.rows[id.Row].group.playlist
	g.clickedAlbum(playlist)
	g.selectedAlbum = playlist
	g.updateAlbumBar()
}

// updateAlbumBar syncs the album controls with the selected album's
// current state — after a selection, and after every list refresh.
func (g *guiApp) updateAlbumBar() {
	if g.selectedAlbum == "" {
		return
	}

	found, paused := false, false
	for _, t := range g.tracks {
		if t.Playlist == g.selectedAlbum {
			found, paused = true, t.Paused
			break
		}
	}
	if !found { // cancelled (or cleaned) since it was selected
		g.selectedAlbum = ""
		g.table.UnselectAll()
		g.albumControls.Hide()
		return
	}

	label := g.selectedAlbum
	if paused {
		label += " (paused)"
		g.pauseBtn.SetText("Resume Album")
		g.pauseBtn.SetIcon(theme.MediaPlayIcon())
	} else {
		g.pauseBtn.SetText("Pause Album")
		g.pauseBtn.SetIcon(theme.MediaPauseIcon())
	}
	g.albumLabel.SetText(label)
	g.albumControls.Show()
}

func (g *guiApp) togglePauseAlbum() {
	if g.selectedAlbum == "" {
		return
	}
	action := "pause"
	if g.pauseBtn.Text == "Resume Album" {
		action = "resume"
	}
	go g.runOneShot(action, g.selectedAlbum)
}

func (g *guiApp) confirmCancelAlbum() {
	name := g.selectedAlbum
	if name == "" {
		return
	}
	dialog.ShowConfirm("Cancel album",
		fmt.Sprintf("Remove %q from the queue?\n\nTracks already downloaded are kept.", name),
		func(ok bool) {
			if ok {
				go g.runOneShot("cancel", name)
			}
		}, g.win)
}
