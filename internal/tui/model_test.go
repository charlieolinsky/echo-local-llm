package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charlesolinsky/local-llm/internal/config"
)

func TestViewRendersDashboard(t *testing.T) {
	cfg := &config.Config{
		Listen:     "0.0.0.0:4000",
		OllamaBase: "http://127.0.0.1:11434",
		APIKey:     "test-secret-key-abcdef",
		Models: map[string]config.ModelEntry{
			"tiny": {Upstream: "smollm2:135m"},
		},
	}
	m := newModel(cfg, newController(cfg, "", ""))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)
	m.gatewayUp = true
	m.ollamaUp = true
	m.status = "running"
	m.logText = "15:04:05  200  POST /v1/chat/completions  12ms  127.0.0.1  tiny"
	m.redrawLogs()

	view := m.View()
	for _, want := range []string{
		"local-llm",
		"gateway",
		"ollama",
		"tiny",
		"4000/v1",
		"space start/stop",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}
