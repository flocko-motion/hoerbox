// package: cmd / cli
// type:    logic
// job:     a muted override for the status colors (defaults read as an eyesore), and progress bar tints
// limits:  only touches success/error/warning; everything else falls through to the default theme
package cmd

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// mutedTheme wraps Fyne's default theme, toning down only the status
// colors (used via widget.Importance in the track table) — the theme
// defaults are saturated enough to be an eyesore across a whole column.
type mutedTheme struct {
	fyne.Theme
}

func (m mutedTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	dark := variant == theme.VariantDark
	switch name {
	case theme.ColorNameSuccess:
		if dark {
			return color.NRGBA{R: 0x7a, G: 0xa8, B: 0x86, A: 0xff}
		}
		return color.NRGBA{R: 0x4c, G: 0x82, B: 0x5c, A: 0xff}
	case theme.ColorNameError:
		if dark {
			return color.NRGBA{R: 0xc4, G: 0x8a, B: 0x8a, A: 0xff}
		}
		return color.NRGBA{R: 0xa8, G: 0x5c, B: 0x5c, A: 0xff}
	case theme.ColorNameWarning:
		if dark {
			return color.NRGBA{R: 0xc4, G: 0xb0, B: 0x78, A: 0xff}
		}
		return color.NRGBA{R: 0x9c, G: 0x82, B: 0x40, A: 0xff}
	default:
		return m.Theme.Color(name, variant)
	}
}

// barTint is a progress bar's fill colour, by what it shows. The cycle
// every track goes through follows a traffic light: red waiting (hold),
// amber starting (get ready), green downloading (go). Preparing sits
// outside that cycle in violet, and plain progress (album, overall)
// stays neutral. The "now" card's state word uses the matching colour
// (see phaseBadge).
type barTint int

const (
	tintPreparing   barTint = iota // violet: starting up, resolving albums
	tintStarting                   // amber: contacting YouTube, no data yet
	tintDownloading                // green: data coming in
	tintWaiting                    // red: the pause between downloads
	tintNeutral                    // grey: album and overall progress
)

func (b barTint) color(base fyne.Theme, variant fyne.ThemeVariant) color.Color {
	dark := variant == theme.VariantDark
	switch b {
	case tintPreparing:
		if dark {
			return color.NRGBA{R: 0xa0, G: 0x8c, B: 0xc4, A: 0xff}
		}
		return color.NRGBA{R: 0x6f, G: 0x5a, B: 0x9c, A: 0xff}
	case tintStarting:
		return base.Color(theme.ColorNameWarning, variant)
	case tintDownloading:
		return base.Color(theme.ColorNameSuccess, variant)
	case tintWaiting:
		return base.Color(theme.ColorNameError, variant)
	default:
		if dark {
			return color.NRGBA{R: 0x7c, G: 0x82, B: 0x8c, A: 0xff}
		}
		return color.NRGBA{R: 0x8a, G: 0x8f, B: 0x98, A: 0xff}
	}
}

// tintTheme recolours one widget's primary colour — what a progress bar
// fills with.
type tintTheme struct {
	fyne.Theme
	tint barTint
}

func (t tintTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return t.tint.color(t.Theme, variant)
	}
	return t.Theme.Color(name, variant)
}

// tinted wraps obj so its primary colour is tint's.
func tinted(obj fyne.CanvasObject, tint barTint) *container.ThemeOverride {
	return container.NewThemeOverride(obj, tintTheme{Theme: mutedTheme{Theme: theme.DefaultTheme()}, tint: tint})
}
