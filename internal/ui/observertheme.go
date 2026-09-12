package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ObserverTheme is a red-on-black theme that preserves night vision at the
// telescope, toggled from the main window. Font, icon, and size lookups are
// delegated to the default dark theme; only colors are overridden.
type ObserverTheme struct{}

var _ fyne.Theme = ObserverTheme{}

func (ObserverTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x10, G: 0x00, B: 0x00, A: 0xff}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xff, G: 0x30, B: 0x30, A: 0xff}
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x30, G: 0x08, B: 0x08, A: 0xff}
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff}
	case theme.ColorNameHover, theme.ColorNameFocus:
		return color.NRGBA{R: 0x50, G: 0x10, B: 0x10, A: 0xff}
	case theme.ColorNameDisabled, theme.ColorNameDisabledButton:
		return color.NRGBA{R: 0x40, G: 0x15, B: 0x15, A: 0xff}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 0x90, G: 0x40, B: 0x40, A: 0xff}
	case theme.ColorNameScrollBar, theme.ColorNameSeparator:
		return color.NRGBA{R: 0x50, G: 0x10, B: 0x10, A: 0xff}
	default:
		return theme.DarkTheme().Color(name, variant)
	}
}

func (ObserverTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DarkTheme().Font(style)
}

func (ObserverTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DarkTheme().Icon(name)
}

func (ObserverTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DarkTheme().Size(name)
}
