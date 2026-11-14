package ytmusic

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		out  string
		want FailureKind
	}{
		{"ERROR: [youtube] ZDPmMpWoGV8: Video unavailable. This video is not available", FailureUnavailable},
		{"ERROR: [youtube] x: Private video. Sign in if you've been granted access to this video", FailureUnavailable},
		{"ERROR: [youtube] x: Video unavailable. This content isn't available, try again later.", FailureBlocked},
		{"ERROR: [youtube] x: Sign in to confirm you're not a bot.", FailureBlocked},
		{"ERROR: [youtube] x: The uploader has not made this video available in your country", FailureUnavailable},
		{"ERROR: [youtube] x: Video unavailable. This video is not available in your country", FailureUnavailable},
		{"ERROR: unable to download video data: HTTP Error 403: Forbidden", FailureBlocked},
		{"ERROR: something new and weird", FailureOther},
	}
	for _, c := range cases {
		if got := classify(c.out); got != c.want {
			t.Errorf("classify(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}
