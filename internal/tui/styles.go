package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent = lipgloss.Color("212")
	colorMuted  = lipgloss.Color("243")
	colorOk     = lipgloss.Color("114")
	colorBad    = lipgloss.Color("203")
	colorWarn  = lipgloss.Color("215")
	colorTitle = lipgloss.Color("231")

	styleApp = lipgloss.NewStyle().Padding(0, 1)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorTitle)

	styleAccent = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	styleMuted = lipgloss.NewStyle().Foreground(colorMuted)

	styleOk = lipgloss.NewStyle().Foreground(colorOk).Bold(true)

	styleBad = lipgloss.NewStyle().Foreground(colorBad).Bold(true)

	styleWarn = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)

	styleHelp = lipgloss.NewStyle().Foreground(colorMuted)

	styleYou = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	styleBot = lipgloss.NewStyle().Foreground(colorOk).Bold(true)

	styleErr = lipgloss.NewStyle().Foreground(colorBad)
)
