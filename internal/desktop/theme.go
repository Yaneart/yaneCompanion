package desktop

import _ "embed"

//go:embed banner-light.png
var lightBanner []byte

type ThemeMode string

const (
	ThemeSystem ThemeMode = "system"
	ThemeLight  ThemeMode = "light"
	ThemeDark   ThemeMode = "dark"
)

var themeChoices = [...]struct {
	mode  ThemeMode
	label string
}{
	{ThemeSystem, "Системная"},
	{ThemeLight, "Светлая"},
	{ThemeDark, "Тёмная"},
}

func (mode ThemeMode) valid() bool {
	return mode == ThemeSystem || mode == ThemeLight || mode == ThemeDark
}

func (mode ThemeMode) dark(systemDark bool) bool {
	return mode == ThemeDark || mode != ThemeLight && systemDark
}

type themeColor uint32

type palette struct {
	background, surface, elevated, border  themeColor
	ink, muted, accent, pressed            themeColor
	action, actionText                     themeColor
	selected, selectedBorder, selectedText themeColor
	closeFill, closeBorder, closeText      themeColor
}

func desktopPalette(dark bool) palette {
	if dark {
		return palette{
			background: 0x101112, surface: 0x191a1b, elevated: 0x222325, border: 0x303235,
			ink: 0xf1f0ec, muted: 0x999b9d, accent: 0x6f94df, pressed: 0x394355,
			action: 0xb5cdfa, actionText: 0x101c32,
			selected: 0x202c42, selectedBorder: 0x34496c, selectedText: 0xbdd3fb,
			closeFill: 0x58242a, closeBorder: 0x78323a, closeText: 0xffd8db,
		}
	}
	// Light colors come from the yaneMedia theme. Its action text color
	// also works as a panel surface behind secondary text.
	return palette{
		background: 0xebeae5, surface: 0xf4f3ef, elevated: 0xffffff, border: 0xbdbbb4,
		ink: 0x18191a, muted: 0x65676a, accent: 0x12447f, pressed: 0xb8bbbd,
		action: 0x18191a, actionText: 0xf4f3ef,
		selected: 0xc9d1d8, selectedBorder: 0x96acc5, selectedText: 0x174f96,
		closeFill: 0xfce1e3, closeBorder: 0xf4ccd0, closeText: 0x92202c,
	}
}
