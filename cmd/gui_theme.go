// package: cmd / cli
// type:    logic
// job:     a muted override for the status colors the track table uses (defaults read as an eyesore)
// limits:  only touches success/error/warning; everything else falls through to the default theme
package cmd

import (
	"image/color"

	"fyne.io/fyne/v2"
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
