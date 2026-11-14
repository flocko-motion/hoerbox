package cmd

import (
	"testing"
	"time"
)

func TestTidyLogLine(t *testing.T) {
	// logrus writes local time; the tidied line shows it as 15:04:05.
	stamp := time.Date(2026, 11, 14, 19, 29, 17, 0, time.Local)
	ts := stamp.Format(time.RFC3339)
	cases := map[string]string{
		`time="` + ts + `" level=info msg="[youtube] ZDPmMpWoGV8: Downloading web music player API JSON" proc=yt-dlp`: "19:29:17 yt-dlp · [youtube] ZDPmMpWoGV8: Downloading web music player API JSON",
		`time="` + ts + `" level=warning msg="say \"hi\"" proc=ffmpeg`:                                                `19:29:17 WARNING ffmpeg · say "hi"`,
		`time="` + ts + `" level=info msg=done`:                                                                       "19:29:17 done",
		`  ! failed: yt-dlp: exit status 1  `:                                                                         "  ! failed: yt-dlp: exit status 1",
		`[2/30] Kapitel 01 — Bibi Blocksberg (4m12s)`:                                                                 "[2/30] Kapitel 01 — Bibi Blocksberg (4m12s)",
	}
	for in, want := range cases {
		if got := tidyLogLine(in); got != want {
			t.Errorf("tidyLogLine(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}
