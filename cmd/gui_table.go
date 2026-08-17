// package: cmd / cli
// type:    logic
// job:     the sortable, color-coded track table — columns, cell rendering, click-to-sort
// limits:  sorts in place on the client side; large libraries would want the sort server-side
package cmd

import (
	"sort"
	"strconv"

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
var columnWidths = [numColumns]float32{160, 220, 70, 280, 160, 160}

func (g *guiApp) buildTable() {
	g.table = widget.NewTable(
		func() (int, int) { return len(g.tracks), numColumns },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			g.updateCell(g.tracks[id.Row], id.Col, o.(*widget.Label))
		},
	)
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

func (g *guiApp) updateCell(t TrackStatus, col int, l *widget.Label) {
	l.Text = cellText(t, col)
	l.Importance = rowImportance(t.Status)
	l.Refresh()
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
		if t.Error != "" {
			return t.Status + ": " + t.Error
		}
		return t.Status
	default:
		return ""
	}
}

func rowImportance(status string) widget.Importance {
	switch status {
	case "succeeded":
		return widget.SuccessImportance
	case "failed":
		return widget.DangerImportance
	case "missing":
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
	g.sortTracks()
	g.table.Refresh()
}

// sortTracks applies the current sortCol/sortAsc in place; a no-op while
// sortCol is -1 (nothing clicked yet — keep the server's playlist-grouped,
// newest-added-first order).
func (g *guiApp) sortTracks() {
	if g.sortCol < 0 {
		return
	}
	col := g.sortCol

	sort.SliceStable(g.tracks, func(i, j int) bool {
		var less bool
		if col == colTrackNo {
			less = g.tracks[i].TrackNo < g.tracks[j].TrackNo
		} else {
			less = cellText(g.tracks[i], col) < cellText(g.tracks[j], col)
		}
		if g.sortAsc {
			return less
		}
		return !less
	})
}
