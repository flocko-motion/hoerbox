// package: ytmusic / integration
// type:    logic
// job:     streams one track's audio to raw PCM via yt-dlp piped into ffmpeg
// limits:  no retry on subprocess failure; caller decides what to do with a failed track
package ytmusic

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/flocko-motion/hoerbox/internal/config"
)

// StreamTrack fetches url via yt-dlp, decodes to raw PCM via ffmpeg (piped
// directly between the two, never buffered by hoerbox itself), and writes
// the result to outputPath — a plain file, or a FIFO with a reader
// attached already (see internal/pcmpipe).
func StreamTrack(ctx context.Context, log *logrus.Entry, cfg *config.Config, url string, outputPath string) error {
	ytArgs := append([]string{"-f", "bestaudio", "-o", "-", "--no-warnings"}, cookieArgs(log, cfg)...)
	ytArgs = append(ytArgs, url)
	ytCmd := exec.CommandContext(ctx, "yt-dlp", ytArgs...)

	ffArgs := []string{"-hide_banner", "-loglevel", "error", "-i", "pipe:0"}
	if strings.HasSuffix(outputPath, ".wav") {
		// A self-describing container for human inspection/playback (double-click,
		// no format flags needed) — ffmpeg's raw PCM codec names (s16le, s32le,
		// f32le) double as its WAV codec names with a "pcm_" prefix.
		ffArgs = append(ffArgs, "-c:a", "pcm_"+cfg.Audio.OutputFormat)
	} else {
		// Headerless raw PCM: what the continuous-pipe hand-off mode needs.
		ffArgs = append(ffArgs, "-f", cfg.Audio.OutputFormat)
	}
	ffArgs = append(ffArgs,
		"-ar", strconv.Itoa(cfg.Audio.SampleRate),
		"-ac", strconv.Itoa(cfg.Audio.Channels),
		"-y", outputPath,
	)
	ffCmd := exec.CommandContext(ctx, "ffmpeg", ffArgs...)

	pipe, err := ytCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("wiring yt-dlp stdout to ffmpeg: %w", err)
	}
	ffCmd.Stdin = pipe

	ytStderr := newTailLogger(log.WithField("proc", "yt-dlp"))
	ffStderr := newTailLogger(log.WithField("proc", "ffmpeg"))
	ytCmd.Stderr = ytStderr
	ffCmd.Stderr = ffStderr

	if err := ytCmd.Start(); err != nil {
		return fmt.Errorf("starting yt-dlp: %w", err)
	}
	if err := ffCmd.Start(); err != nil {
		_ = ytCmd.Process.Kill()
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	ytErr := ytCmd.Wait()
	ffErr := ffCmd.Wait()

	if ytErr != nil {
		return fmt.Errorf("yt-dlp: %w: %s", ytErr, ytStderr.tail())
	}
	if ffErr != nil {
		return fmt.Errorf("ffmpeg: %w: %s", ffErr, ffStderr.tail())
	}
	return nil
}

// tailLogger writes each line it receives to log at info level — on by
// default, this is hoerbox's "what's happening" narration for a
// subprocess it doesn't control the output format of — while also
// keeping the lines around so a failure can report real subprocess
// output instead of just an exit code.
type tailLogger struct {
	log  *logrus.Entry
	buf  bytes.Buffer
	w    *io.PipeWriter
	done chan struct{}
}

func newTailLogger(log *logrus.Entry) *tailLogger {
	r, w := io.Pipe()
	t := &tailLogger{log: log, w: w, done: make(chan struct{})}
	go func() {
		defer close(t.done)
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			t.log.Info(line)
			t.buf.WriteString(line)
			t.buf.WriteByte('\n')
		}
		if err := scanner.Err(); err != nil {
			t.log.WithError(err).Debug("subprocess output scan ended early")
		}
	}()
	return t
}

func (t *tailLogger) Write(p []byte) (int, error) { return t.w.Write(p) }

// tail closes the writer (so the reader goroutine sees EOF), waits for it
// to finish draining every already-written line, and only then reads buf —
// otherwise this would race the goroutine still writing to it.
func (t *tailLogger) tail() string {
	_ = t.w.Close()
	<-t.done
	return strings.TrimSpace(t.buf.String())
}
