// package: ytmusic / integration
// type:    logic
// job:     translates cfg.YouTube into yt-dlp's cookie-auth flags
// limits:  no validation that a configured cookies file/browser actually works
package ytmusic

import (
	"github.com/sirupsen/logrus"

	"github.com/flocko-motion/hoerbox/internal/config"
)

// KnownCookieBrowsers is yt-dlp's own supported set for
// --cookies-from-browser (see `yt-dlp --help`) — kept in sync by hand,
// since yt-dlp has no machine-readable way to ask for it.
var KnownCookieBrowsers = []string{
	"brave", "chrome", "chromium", "edge", "firefox", "opera", "safari", "vivaldi", "whale",
}

// cookieArgs returns the yt-dlp flags needed to authenticate as the
// account configured in cfg.YouTube, or nil if none is configured (fine
// for public/unlisted content, but most real tracks in an account's own
// library turned out to require this — see package doc).
func cookieArgs(log *logrus.Entry, cfg *config.Config) []string {
	switch {
	case cfg.YouTube.CookiesFile != "":
		log.Infof("using cookies file: %s", cfg.YouTube.CookiesFile)
		return []string{"--cookies", cfg.YouTube.CookiesFile}
	case cfg.YouTube.CookiesFromBrowser != "":
		log.Infof("scanning %s for cookies", cfg.YouTube.CookiesFromBrowser)
		return []string{"--cookies-from-browser", cfg.YouTube.CookiesFromBrowser}
	default:
		return nil
	}
}
