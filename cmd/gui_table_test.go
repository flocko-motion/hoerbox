package cmd

import "testing"

func TestRebuildRowsGroupsAndUnfoldsOneAlbum(t *testing.T) {
	g := &guiApp{sortCol: -1, tracks: []TrackStatus{
		{Playlist: "b", TrackNo: 1, Status: "succeeded"},
		{Playlist: "a", TrackNo: 1, Status: "succeeded"},
		{Playlist: "b", TrackNo: 2, Status: "failed"},
		{Playlist: "a", TrackNo: 2, Status: "unavailable"},
	}}

	g.rebuildRows()
	if len(g.rows) != 2 || g.rows[0].group.playlist != "b" || g.rows[1].group.playlist != "a" {
		t.Fatalf("folded: want album rows b, a in server order, got %d rows", len(g.rows))
	}

	g.expandedAlbum = "b"
	g.rebuildRows()
	if len(g.rows) != 4 || g.rows[1].track == nil || g.rows[3].track != nil {
		t.Fatalf("b unfolded: want b, b1, b2, a; got %d rows", len(g.rows))
	}

	if got := albumStatus(g.rows[0].group); got != "1 failed" {
		t.Errorf("albumStatus(b) = %q", got)
	}
	if got := albumStatus(g.rows[3].group); got != "1 unavailable" {
		t.Errorf("albumStatus(a) = %q", got)
	}
	if got := albumCellText(g.rows[3].group, colTrackName, false); got != "1/2 tracks downloaded" {
		t.Errorf("album progress = %q", got)
	}
}

func TestRebuildRowsDescendingKeepsTiesStable(t *testing.T) {
	g := &guiApp{sortCol: colStatus, sortAsc: false, tracks: []TrackStatus{
		{Playlist: "first", Status: "succeeded"},
		{Playlist: "second", Status: "succeeded"},
		{Playlist: "third", Status: "succeeded"},
	}}
	g.rebuildRows()
	for i, want := range []string{"first", "second", "third"} {
		if got := g.rows[i].group.playlist; got != want {
			t.Fatalf("row %d = %q, want %q (ties must keep server order)", i, got, want)
		}
	}
}
