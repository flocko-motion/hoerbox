// package: cmd / cli
// type:    logic
// job:     the GUI's "now" panel, last-error banner, and album controls (pause/resume/cancel)
// limits:  polls data/status.json twice a second — whichever process is streaming, ours or not
package cmd

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/flocko-motion/hoerbox/internal/runstate"
)

// buildStatusPanel creates the widgets between the toolbar and the
// table: what's happening now, the last error, and the selected album's
// controls.
func (g *guiApp) buildStatusPanel() fyne.CanvasObject {
	g.nowTitle = widget.NewLabel("idle")
	g.nowTitle.TextStyle = fyne.TextStyle{Bold: true}
	g.nowTitle.Truncation = fyne.TextTruncateEllipsis
	g.nowDetail = widget.NewLabel("")
	g.nowDetail.Importance = widget.LowImportance
	g.nowDetail.Truncation = fyne.TextTruncateEllipsis

	g.errLabel = widget.NewLabel("")
	g.errLabel.Importance = widget.DangerImportance
	g.errLabel.Wrapping = fyne.TextWrapWord
	dismiss := widget.NewButton("Dismiss", func() {
		g.errDismissedAt = time.Now()
		g.errBanner.Hide()
	})
	g.errBanner = container.NewBorder(nil, nil, nil, dismiss, g.errLabel)
	g.errBanner.Hide()

	g.albumLabel = widget.NewLabel("Select an album to pause, resume, or cancel it (double-click it to show its tracks)")
	g.albumLabel.Truncation = fyne.TextTruncateEllipsis
	g.pauseBtn = widget.NewButton("Pause Album", g.togglePauseAlbum)
	g.cancelBtn = widget.NewButton("Cancel Album", g.confirmCancelAlbum)
	g.cancelBtn.Importance = widget.DangerImportance
	g.pauseBtn.Disable()
	g.cancelBtn.Disable()
	albumBar := container.NewBorder(nil, nil, nil, container.NewHBox(g.pauseBtn, g.cancelBtn), g.albumLabel)

	return container.NewVBox(g.nowTitle, g.nowDetail, g.errBanner, widget.NewSeparator(), albumBar)
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

	switch {
	case own:
		g.startStopBtn.SetText("Stop")
		g.startStopBtn.Enable()
	case s.Running:
		g.startStopBtn.SetText(fmt.Sprintf("Running (pid %d)", s.PID))
		g.startStopBtn.Disable()
	default:
		g.startStopBtn.SetText("Start")
		g.startStopBtn.Enable()
	}

	switch {
	case !ok:
		g.nowTitle.SetText("Idle — nothing has run yet")
		g.nowDetail.SetText("")
	case s.Running:
		title := s.Message
		if s.Track != nil && s.Track.Playlist != "" {
			title += "   [" + s.Track.Playlist + "]"
		}
		g.nowTitle.SetText(title)
		detail := fmt.Sprintf("running %s (pid %d)", ago(s.StartedAt, now), s.PID)
		if !own {
			detail += ", started outside this window"
		}
		if s.Counts.Total > 0 {
			detail = describeCounts(s.Counts) + " · " + detail
		}
		g.nowDetail.SetText(detail)
	default:
		lines := describeStatus(s, now)
		g.nowTitle.SetText(strings.TrimSpace(lines[0]))
		g.nowDetail.SetText(strings.TrimSpace(strings.Join(lines[1:], " · ")))
	}

	// A "stream" this window doesn't own only tells us about progress
	// here; refresh the table when its numbers move.
	if !own && ok && (s.Counts != g.lastCounts || s.Running != g.lastRunning) {
		go g.refreshList()
	}
	g.lastCounts, g.lastRunning = s.Counts, s.Running

	g.showLastError(s.LastError)
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
		g.albumLabel.SetText("Select an album to pause, resume, or cancel it (double-click it to show its tracks)")
		g.pauseBtn.Disable()
		g.cancelBtn.Disable()
		return
	}

	label := "Album: " + g.selectedAlbum
	if paused {
		label += " (paused)"
		g.pauseBtn.SetText("Resume Album")
	} else {
		g.pauseBtn.SetText("Pause Album")
	}
	g.albumLabel.SetText(label)
	g.pauseBtn.Enable()
	g.cancelBtn.Enable()
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
