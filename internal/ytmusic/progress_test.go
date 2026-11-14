package ytmusic

import "testing"

func TestParseProgress(t *testing.T) {
	cases := []struct {
		line       string
		done, size int64
		ok         bool
	}{
		{"hoerbox-progress 1024 4096 NA", 1024, 4096, true},
		{"hoerbox-progress 1024 NA 5000.5", 1024, 5000, true},
		{"hoerbox-progress 0 NA NA", 0, 0, true},
		{"[youtube] abc: Downloading webpage", 0, 0, false},
	}
	for _, c := range cases {
		done, size, ok := parseProgress(c.line)
		if done != c.done || size != c.size || ok != c.ok {
			t.Errorf("parseProgress(%q) = %d, %d, %v", c.line, done, size, ok)
		}
	}
}
