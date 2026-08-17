// package: cmd / cli
// type:    logic
// job:     subprocess plumbing for the GUI — running/stopping "stream", running one-shots
// limits:  all of it assumes it's only ever driven by cmd/gui.go
package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// lineSink buffers to each newline and calls onLine per line. Safe as
// both Stdout and Stderr at once: os/exec copies each concurrently.
type lineSink struct {
	mu     sync.Mutex
	buf    []byte
	onLine func(string)
}

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		line := string(s.buf[:i])
		s.buf = s.buf[i+1:]
		s.onLine(line)
	}
	return len(p), nil
}

// toggleStream is the Start/Stop button's handler: it starts "stream" if
// nothing's running, or signals the running one to stop gracefully.
func (g *guiApp) toggleStream() {
	g.mu.Lock()
	running := g.running
	g.mu.Unlock()

	if running != nil {
		_ = running.Process.Signal(os.Interrupt) // graceful: cascades to yt-dlp/ffmpeg, see cmd/root.go
		return
	}
	g.startStream()
}

// startStream launches "stream" with a watch pipe (see watchParentPipe)
// bound to fd 3: as long as we hold the write end open, the child knows
// we're alive; the moment we're gone — clean quit, crash, force-quit
// alike — the OS closes it for us and the child signals itself to stop,
// so it never outlives this GUI as an orphan.
func (g *guiApp) startStream() {
	cmd := exec.Command(g.self, "stream")
	sink := &lineSink{onLine: g.handleLine}
	cmd.Stdout, cmd.Stderr = sink, sink

	watchRead, watchWrite, err := os.Pipe()
	if err != nil {
		dialog.ShowError(fmt.Errorf("starting stream: %w", err), g.win)
		return
	}
	cmd.ExtraFiles = []*os.File{watchRead}
	cmd.Env = append(os.Environ(), parentWatchFDEnv+"=3")

	if err := cmd.Start(); err != nil {
		_ = watchRead.Close()
		_ = watchWrite.Close()
		dialog.ShowError(fmt.Errorf("starting stream: %w", err), g.win)
		return
	}
	_ = watchRead.Close() // the child has its own copy; ours is no longer needed

	g.mu.Lock()
	g.running = cmd
	g.mu.Unlock()
	fyne.Do(func() { g.startStopBtn.SetText("Stop") })

	go func() {
		waitErr := cmd.Wait()
		_ = watchWrite.Close()

		g.mu.Lock()
		g.running = nil
		g.mu.Unlock()

		fyne.Do(func() {
			g.startStopBtn.SetText("Start")
			if waitErr != nil {
				g.statusLabel.SetText("stopped: " + waitErr.Error())
			} else {
				g.statusLabel.SetText("idle")
			}
		})
		g.refreshList()
	}()
}

// handleLine is the running "stream" subprocess's per-line callback: a
// token line (cmd/consoleline.go) updates status/triggers a refresh;
// anything else is narration, appended verbatim.
func (g *guiApp) handleLine(line string) {
	if rest, ok := strings.CutPrefix(line, tokenPrefix+" "); ok {
		kind, text, _ := strings.Cut(rest, " ")
		switch kind {
		case "status":
			fyne.Do(func() { g.statusLabel.SetText(text) })
		case "refresh":
			g.refreshList()
		}
		return
	}
	fyne.Do(func() { g.appendTerminal(line) })
}

// maxTerminalLines caps the terminal pane's buffer so a long-running
// stream can't grow it (and the plain string-concat appendTerminal does)
// without bound.
const maxTerminalLines = 1000

// appendTerminal adds a line (capped to maxTerminalLines) and follows the
// bottom only if the view was already there.
func (g *guiApp) appendTerminal(line string) {
	stick := g.scrolledToBottom()

	text := g.terminal.Text + line + "\n"
	if lines := strings.Split(text, "\n"); len(lines) > maxTerminalLines+1 {
		text = strings.Join(lines[len(lines)-maxTerminalLines-1:], "\n")
	}
	g.terminal.SetText(text)

	if stick {
		g.terminalPanel.ScrollToBottom()
	}
}

// scrolledToBottom reports whether the terminal is scrolled to (or near)
// its bottom right now.
func (g *guiApp) scrolledToBottom() bool {
	maxY := g.terminalPanel.Content.Size().Height - g.terminalPanel.Size().Height
	if maxY <= 0 {
		return true // everything fits, nothing to scroll
	}
	const tolerance = float32(4)
	return g.terminalPanel.Offset.Y >= maxY-tolerance
}

// refreshList re-runs "list --json" (a fast, cache-only, network-free
// read) and updates the list widget. Safe to call from any goroutine.
func (g *guiApp) refreshList() {
	cmd := exec.Command(g.self, "list", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		fyne.Do(func() { g.appendTerminal(fmt.Sprintf("! list --json failed: %s", strings.TrimSpace(stderr.String()))) })
		return
	}

	var tracks []TrackStatus
	if err := json.Unmarshal(out, &tracks); err != nil {
		fyne.Do(func() { g.appendTerminal(fmt.Sprintf("! list --json: unexpected output: %s", err)) })
		return
	}

	fyne.Do(func() {
		g.tracks = tracks
		g.sortTracks() // keep whatever column the user last sorted by
		g.table.Refresh()
	})
}

// showAddDialog prompts for a URL and runs "add" with it in the
// background.
func (g *guiApp) showAddDialog() {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("e.g. https://music.youtube.com/playlist?list=...")

	d := dialog.NewForm("Add playlist", "Add", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("URL", entry)},
		func(ok bool) {
			if !ok || strings.TrimSpace(entry.Text) == "" {
				return
			}
			go g.runOneShot("add", entry.Text)
		}, g.win)
	d.Resize(fyne.NewSize(560, 160))
	d.Show()
}

// removeDone runs "clean" in the background.
func (g *guiApp) removeDone() {
	go g.runOneShot("clean")
}

// runOneShot runs a hoerbox subcommand to completion, appends its
// combined output to the terminal view, shows an error dialog if it
// failed, and refreshes the list either way.
func (g *guiApp) runOneShot(args ...string) {
	var out bytes.Buffer
	cmd := exec.Command(g.self, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()

	fyne.Do(func() {
		g.appendTerminal(strings.TrimRight(out.String(), "\n"))
		if err != nil {
			dialog.ShowError(fmt.Errorf("%s: %w", strings.Join(args, " "), err), g.win)
		}
	})
	g.refreshList()
}
