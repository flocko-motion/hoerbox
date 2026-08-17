// package: downloadlog / integration
// type:    logic
// job:     records per-track download attempts to out/download-log.json for resumable retries
// limits:  reloads the whole file on every call — fine at whitelist scale, not huge libraries
package downloadlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Status is the outcome of one recorded download attempt.
type Status string

const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
)

// Entry is one recorded download attempt, keyed by URL in a Log.
type Entry struct {
	URL         string    `json:"url"`
	TrackName   string    `json:"trackname"`
	Artist      string    `json:"artist"`
	Album       string    `json:"album"`
	File        string    `json:"file"`
	Status      Status    `json:"status"`
	Error       string    `json:"error,omitempty"`
	AttemptedAt time.Time `json:"attempted_at"`
}

// Log points at the download manifest for one output directory. It holds
// no state of its own — every call re-reads the file fresh, since it may
// have been hand-edited, or updated by another run, since it was last
// looked at.
type Log struct {
	path string
}

const fileName = "download-log.json"

// Open returns a Log for dir's download-log.json. dir need not exist yet
// or contain a log yet — reads before any entry has been recorded just
// see an empty log.
func Open(dir string) *Log {
	return &Log{path: filepath.Join(dir, fileName)}
}

// Get looks up the most recent attempt for a track URL, reading the log
// fresh from disk.
func (l *Log) Get(url string) (Entry, bool, error) {
	entries, err := l.load()
	if err != nil {
		return Entry{}, false, err
	}
	e, ok := entries[url]
	return e, ok, nil
}

// Record stores e, merging into whatever is currently on disk (in case it
// changed since anything here last read it) before persisting.
func (l *Log) Record(e Entry) error {
	entries, err := l.load()
	if err != nil {
		return err
	}
	entries[e.URL] = e
	return l.save(entries)
}

// All returns every recorded attempt, keyed by URL, read fresh from disk.
// Prefer this over repeated Get calls when checking many URLs at once —
// each Get re-reads the whole file, which adds up.
func (l *Log) All() (map[string]Entry, error) {
	return l.load()
}

// Delete removes urls' entries (if present) and persists the log.
func (l *Log) Delete(urls []string) error {
	entries, err := l.load()
	if err != nil {
		return err
	}
	for _, u := range urls {
		delete(entries, u)
	}
	return l.save(entries)
}

func (l *Log) load() (map[string]Entry, error) {
	entries := map[string]Entry{}

	content, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, fmt.Errorf("reading %s: %w", l.path, err)
	}

	var list []Entry
	if err := json.Unmarshal(content, &list); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", l.path, err)
	}
	for _, e := range list {
		entries[e.URL] = e
	}
	return entries, nil
}

func (l *Log) save(entries map[string]Entry) error {
	content, err := json.MarshalIndent(sortedByAttempt(entries), "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling download log: %w", err)
	}
	if err := os.WriteFile(l.path, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", l.path, err)
	}
	return nil
}

func sortedByAttempt(entries map[string]Entry) []Entry {
	list := make([]Entry, 0, len(entries))
	for _, e := range entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].AttemptedAt.Before(list[j].AttemptedAt) })
	return list
}
