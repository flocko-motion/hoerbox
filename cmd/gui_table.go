// package: cmd / cli
// type:    logic
// job:     the album-grouped track table: double-click unfolds one album at a time; sortable, color-coded
// limits:  sorts in place on the client side; large libraries would want the sort server-side
package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

const (
	colArtist = iota
	colAlbum
	colTrackNo
	colTrackName
	colPlaylist
	colStatus
	numColumns
)

var columnHeaders = [numColumns]string{"Artist", "Album", "Track No", "Track Name", "Playlist", "Status"}
var columnWidths = [numColumns]float32{160, 220, 70, 280, 160, 220}

// albumGroup is one album (./in/<album>/) and its tracks: what the table
// shows as one foldable album row.
type albumGroup struct {
	playlist string
	tracks   []TrackStatus
}

// tableRow is one visible table row: an album header (track == nil), or
// one track of an unfolded album.
type tableRow struct {
	group *albumGroup
	track *TrackStatus
}

func (g *guiApp) buildTable() {
	g.table = widget.NewTable(
		func() (int, int) { return len(g.rows), numColumns },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			g.updateCell(g.rows[id.Row], id.Col, o.(*widget.Label))
		},
	)
	g.table.OnSelected = g.selectTrackRow
	g.table.ShowHeaderRow = true
	g.table.CreateHeader = func() fyne.CanvasObject { return widget.NewButton("", nil) }
	g.table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		col := id.Col
		b := o.(*widget.Button)
		b.SetText(g.headerLabel(col))
		b.OnTapped = func() { g.onHeaderTapped(col) }
	}
	for col, w := range columnWidths {
		g.table.SetColumnWidth(col, w)
	}
}

// doubleClickWindow is how close two clicks on the same album must be to
// count as a double-click (Fyne's Table has no double-tap of its own).
const doubleClickWindow = 400 * time.Millisecond

// clickedAlbum handles a click on any row of playlist's album: a second
// click within doubleClickWindow unfolds it (folding whichever album was
// open) or folds it again. It returns whether it was a double-click.
func (g *guiApp) clickedAlbum(playlist string) bool {
	now := time.Now()
	double := playlist == g.lastClickAlbum && now.Sub(g.lastClickAt) < doubleClickWindow
	g.lastClickAlbum, g.lastClickAt = playlist, now
	if !double {
		return false
	}
	g.lastClickAlbum = "" // a third click starts a new pair
	if g.expandedAlbum == playlist {
		g.expandedAlbum = ""
	} else {
		g.expandedAlbum = playlist
	}
	g.rebuildRows()
	g.table.Refresh()
	return true
}

func (g *guiApp) updateCell(r tableRow, col int, l *widget.Label) {
	if r.track == nil {
		l.Text = albumCellText(r.group, col, g.expandedAlbum == r.group.playlist)
		l.TextStyle = fyne.TextStyle{Bold: true}
		l.Importance = albumImportance(r.group)
	} else {
		t := *r.track
		l.Text = cellText(t, col)
		if col == colTrackName {
			l.Text = "    " + l.Text
		}
		l.TextStyle = fyne.TextStyle{}
		l.Importance = rowImportance(t.Status)
		if pausedTrack(t) {
			l.Importance = widget.LowImportance
		}
	}
	l.Refresh()
}

// pausedTrack is whether t still needs downloading but its album is
// paused — a finished track stays shown as finished.
func pausedTrack(t TrackStatus) bool {
	return t.Paused && t.Status != "succeeded"
}

func cellText(t TrackStatus, col int) string {
	switch col {
	case colArtist:
		return t.Artist
	case colAlbum:
		return t.Album
	case colTrackNo:
		return strconv.Itoa(t.TrackNo)
	case colTrackName:
		return t.TrackName
	case colPlaylist:
		return t.Playlist
	case colStatus:
		if pausedTrack(t) {
			return "paused (" + t.Status + ")"
		}
		if t.Error != "" {
			return t.Status + ": " + oneLine(t.Error)
		}
		return t.Status
	default:
		return ""
	}
}

// albumCounts tallies an album's tracks by TrackStatus.Status.
func albumCounts(a *albumGroup) map[string]int {
	n := map[string]int{}
	for _, t := range a.tracks {
		n[t.Status]++
	}
	return n
}

func albumCellText(a *albumGroup, col int, expanded bool) string {
	switch col {
	case colArtist:
		return commonField(a.tracks, func(t TrackStatus) string { return t.Artist }, "various artists")
	case colAlbum:
		arrow := "▶ "
		if expanded {
			arrow = "▼ "
		}
		return arrow + commonField(a.tracks, func(t TrackStatus) string { return t.Album }, a.playlist)
	case colTrackNo:
		return strconv.Itoa(len(a.tracks))
	case colTrackName:
		n := albumCounts(a)
		return fmt.Sprintf("%d/%d tracks downloaded", n["succeeded"], len(a.tracks))
	case colPlaylist:
		return a.playlist
	case colStatus:
		return albumStatus(a)
	default:
		return ""
	}
}

// albumStatus summarises an album in one line: "done", or what's still
// outstanding, prefixed with "paused" when the album is paused.
func albumStatus(a *albumGroup) string {
	n := albumCounts(a)
	var parts []string
	for _, s := range []string{"untried", "failed", "missing", "unavailable"} {
		if n[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[s], s))
		}
	}
	status := "done"
	if len(parts) > 0 {
		status = strings.Join(parts, " · ")
	}
	if albumPaused(a) && status != "done" {
		status = "paused · " + status
	}
	return status
}

func albumPaused(a *albumGroup) bool {
	return len(a.tracks) > 0 && a.tracks[0].Paused
}

func albumImportance(a *albumGroup) widget.Importance {
	n := albumCounts(a)
	switch {
	case n["succeeded"]+n["unavailable"] == len(a.tracks):
		return widget.SuccessImportance
	case albumPaused(a):
		return widget.LowImportance
	case n["failed"] > 0:
		return widget.DangerImportance
	default:
		return widget.MediumImportance
	}
}

// commonField returns field's value if every track shares it, else fallback.
func commonField(tracks []TrackStatus, field func(TrackStatus) string, fallback string) string {
	if len(tracks) == 0 {
		return fallback
	}
	v := field(tracks[0])
	for _, t := range tracks[1:] {
		if field(t) != v {
			return fallback
		}
	}
	if v == "" {
		return fallback
	}
	return v
}

// oneLine collapses a (possibly multi-line, possibly huge) error into one
// short line for a fixed-height table cell — Truncation alone doesn't
// stop an embedded newline from ballooning the row's own height; the
// terminal pane is where the full story belongs.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const maxLen = 160
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}

func rowImportance(status string) widget.Importance {
	switch status {
	case "succeeded":
		return widget.SuccessImportance
	case "failed":
		return widget.DangerImportance
	case "missing", "unavailable":
		return widget.LowImportance
	default: // "untried"
		return widget.MediumImportance
	}
}

func (g *guiApp) headerLabel(col int) string {
	label := columnHeaders[col]
	switch {
	case g.sortCol != col:
		return label
	case g.sortAsc:
		return label + " ▲"
	default:
		return label + " ▼"
	}
}

// onHeaderTapped is a column header's click handler: first click sorts
// ascending, a second click on the same column reverses it.
func (g *guiApp) onHeaderTapped(col int) {
	if g.sortCol == col {
		g.sortAsc = !g.sortAsc
	} else {
		g.sortCol, g.sortAsc = col, true
	}
	g.rebuildRows()
	g.table.Refresh()
}

// rebuildRows regroups g.tracks into albums (in the server's
// newest-added-first order until a column is sorted), applies the current
// sort to albums and to the tracks inside each, and flattens the result
// into g.rows, unfolding g.expandedAlbum.
func (g *guiApp) rebuildRows() {
	var groups []*albumGroup
	byName := map[string]*albumGroup{}
	for _, t := range g.tracks {
		a := byName[t.Playlist]
		if a == nil {
			a = &albumGroup{playlist: t.Playlist}
			byName[t.Playlist] = a
			groups = append(groups, a)
		}
		a.tracks = append(a.tracks, t)
	}

	if col := g.sortCol; col >= 0 {
		// Descending swaps the operands rather than negating the result,
		// so equal items stay "not less" both ways and keep their order.
		directed := func(less func(i, j int) bool) func(i, j int) bool {
			if g.sortAsc {
				return less
			}
			return func(i, j int) bool { return less(j, i) }
		}
		sort.SliceStable(groups, directed(func(i, j int) bool {
			if col == colTrackNo {
				return len(groups[i].tracks) < len(groups[j].tracks)
			}
			return albumCellText(groups[i], col, false) < albumCellText(groups[j], col, false)
		}))
		for _, a := range groups {
			sort.SliceStable(a.tracks, directed(func(i, j int) bool {
				if col == colTrackNo {
					return a.tracks[i].TrackNo < a.tracks[j].TrackNo
				}
				return cellText(a.tracks[i], col) < cellText(a.tracks[j], col)
			}))
		}
	}

	g.rows = g.rows[:0]
	for _, a := range groups {
		g.rows = append(g.rows, tableRow{group: a})
		if g.expandedAlbum == a.playlist {
			for i := range a.tracks {
				g.rows = append(g.rows, tableRow{group: a, track: &a.tracks[i]})
			}
		}
	}
}
