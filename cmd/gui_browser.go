// package: cmd / cli
// type:    logic
// job:     the GUI's "which browser to read cookies from" picker, persisted via Fyne preferences
// limits:  only ever passed to subcommands the GUI itself launches (stream, add)
package cmd

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/flocko-motion/hoerbox/internal/config"
	"github.com/flocko-motion/hoerbox/internal/ytmusic"
)

// browserPrefKey is the Fyne preferences key the browser picker saves to.
const browserPrefKey = "cookiesBrowser"

// browser returns the GUI's currently configured cookie-source browser —
// whatever was last picked, or config's own default before anyone has.
func (g *guiApp) browser() string {
	if b := g.app.Preferences().String(browserPrefKey); b != "" {
		return b
	}
	return config.DefaultCookiesFromBrowser
}

// browserArgs is the --browser flag to append to any subcommand the GUI
// launches that resolves against YouTube (stream, add).
func (g *guiApp) browserArgs() []string {
	return []string{"--browser", g.browser()}
}

// showBrowserDialog lets the user pick which local browser hoerbox reads
// YouTube cookies from — yt-dlp's own known set, not a free-text field
// that could silently typo into "not found".
func (g *guiApp) showBrowserDialog() {
	sel := widget.NewSelect(ytmusic.KnownCookieBrowsers, nil)
	sel.SetSelected(g.browser())

	d := dialog.NewForm("Cookie browser", "Save", "Cancel",
		[]*widget.FormItem{widget.NewFormItem("Browser", sel)},
		func(ok bool) {
			if !ok || sel.Selected == "" {
				return
			}
			g.app.Preferences().SetString(browserPrefKey, sel.Selected)
		}, g.win)
	d.Resize(fyne.NewSize(360, 120))
	d.Show()
}
