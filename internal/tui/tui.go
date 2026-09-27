package tui

import (
	"fmt"
	"io"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charlesolinsky/local-llm/internal/config"
)

// Run opens the control dashboard. It does not start the gateway by itself;
// use space to start or stop. The gateway is left running if you quit the TUI.
func Run(cfg *config.Config, configPath, envPath string) error {
	prevLog := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(prevLog)

	ctrl := newController(cfg, configPath, envPath)
	p := tea.NewProgram(newModel(cfg, ctrl), tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
