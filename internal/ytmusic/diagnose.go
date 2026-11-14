// package: ytmusic / integration
// type:    logic
// job:     classifies a failed download and probes whether yt-dlp+cookies work at all
// limits:  classification matches yt-dlp's English error text; unknown text is FailureOther
package ytmusic

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/flocko-motion/hoerbox/internal/config"
)

// FailureKind says whose problem a failed download is.
type FailureKind int

const (
	// FailureOther is anything not recognised below.
	FailureOther FailureKind = iota
	// FailureUnavailable is the track itself: removed, private, or
	// region-blocked. Retrying won't help; hoerbox's setup is fine.
	FailureUnavailable
	// FailureBlocked is YouTube refusing hoerbox: bot check, rate limit,
	// auth. Systemic — every following track would fail the same way.
	FailureBlocked
)

// TrackError is StreamTrack's error, carrying its FailureKind.
type TrackError struct {
	Kind FailureKind
	Err  error
}

func (e *TrackError) Error() string { return e.Err.Error() }
func (e *TrackError) Unwrap() error { return e.Err }

// KindOf returns err's FailureKind, FailureOther if it carries none.
func KindOf(err error) FailureKind {
	var te *TrackError
	if errors.As(err, &te) {
		return te.Kind
	}
	return FailureOther
}

// Checked before unavailablePatterns: YouTube's rate limiting also says
// "content isn't available", but adds "try again later".
var blockedPatterns = []string{
	"sign in to confirm",
	"not a bot",
	"try again later",
	"http error 429",
	"http error 403",
	"could not find",    // --cookies-from-browser: no such browser profile/db
	"failed to decrypt", // --cookies-from-browser: keychain refused
	"cookies are no longer valid",
}

var unavailablePatterns = []string{
	"video unavailable",
	"private video",
	"has been removed",
	"no longer available",
	"not available in your country",
	"not made this video available",
	"account associated with this video has been terminated",
}

func classify(ytOutput string) FailureKind {
	s := strings.ToLower(ytOutput)
	for _, p := range blockedPatterns {
		if strings.Contains(s, p) {
			return FailureBlocked
		}
	}
	for _, p := range unavailablePatterns {
		if strings.Contains(s, p) {
			return FailureUnavailable
		}
	}
	return FailureOther
}

// probeVideo is a video that has been up since 2005 and is the stable
// "does YouTube work at all from here" reference.
const probeVideo = "https://www.youtube.com/watch?v=jNQXAC9IVRw"

// Diagnosis is Probe's verdict on hoerbox's own setup.
type Diagnosis struct {
	// Healthy means the reference video downloads with the configured
	// cookies: recent failures are the tracks' fault, not the setup's.
	Healthy bool
	// Fix is a concrete next step for the person when !Healthy.
	Fix string
}

// Probe checks whether yt-dlp can fetch a known-good video, first with
// the configured cookies, then without, to pin the fault on yt-dlp, the
// cookies, or neither.
func Probe(ctx context.Context, log *logrus.Entry, cfg *config.Config) Diagnosis {
	withCookies := cookieArgs(log, cfg)
	if probeOK(ctx, withCookies) {
		return Diagnosis{Healthy: true}
	}
	if len(withCookies) > 0 && probeOK(ctx, nil) {
		return Diagnosis{Fix: "yt-dlp works without cookies but fails with them: the cookies are the problem.\n" +
			"  Open music.youtube.com in " + cookieSource(cfg) + ", make sure you're logged in and can play a track,\n" +
			"  quit the browser fully (so its cookie DB is flushed), then run again."}
	}
	return Diagnosis{Fix: "yt-dlp can't fetch even a known-good video, with or without cookies.\n" +
		"  Most likely yt-dlp is outdated (YouTube changes often): brew upgrade yt-dlp\n" +
		"  If it's current, check the network, or wait an hour in case YouTube is rate-limiting this IP."}
}

func probeOK(ctx context.Context, extra []string) bool {
	args := append([]string{"--simulate", "--quiet", "--no-warnings", "-f", "bestaudio"}, extra...)
	return exec.CommandContext(ctx, "yt-dlp", append(args, probeVideo)...).Run() == nil
}

func cookieSource(cfg *config.Config) string {
	if cfg.YouTube.CookiesFile != "" {
		return "the browser you exported " + cfg.YouTube.CookiesFile + " from"
	}
	return cfg.YouTube.CookiesFromBrowser
}
