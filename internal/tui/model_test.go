package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charlesolinsky/local-llm/internal/config"
)

func testStore(t *testing.T) *config.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	_ = os.WriteFile(path, []byte(`
listen: "0.0.0.0:4000"
ollama_base: "http://127.0.0.1:11434"
active: tiny
context_length: 8192
models:
  tiny:
    upstream: "smollm2:135m"
`), 0o644)
	t.Setenv(config.EnvAPIKey, "test-secret-key-abcdef")
	st, err := config.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func sizedView(t *testing.T, w, h int) string {
	t.Helper()
	st := testStore(t)
	m := newModel(st, newController(st, ""))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = updated.(model)
	m.gatewayUp = true
	m.ollamaUp = true
	m.status = "running"
	m.logText = "15:04:05  200  POST /v1/chat/completions  12ms  127.0.0.1  tiny"
	m.redrawLogs()
	m.redrawList()
	return m.View()
}

func TestViewRendersDashboard(t *testing.T) {
	view := sizedView(t, 100, 40)
	for _, want := range []string{
		"local-llm",
		"gw",
		"ollama",
		"tiny",
		"4000/v1",
		"logs",
		"models",
		"space start/stop",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}

func TestViewFitsSmallAndWide(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {160, 50}} {
		view := sizedView(t, sz[0], sz[1])
		if !strings.Contains(view, "local-llm") || !strings.Contains(view, "1 logs") {
			t.Fatalf("%dx%d missing chrome\n%s", sz[0], sz[1], view)
		}
	}
}

func TestLibraryRowsMarkInstalled(t *testing.T) {
	cfg := config.Config{
		Active: "chat",
		Models: map[string]config.ModelEntry{
			"chat": {Upstream: "qwen3.5:9b"},
			"tiny": {Upstream: "smollm2:135m"},
		},
	}
	rows := buildLibraryRows(config.DefaultLibrary(), []string{"qwen3.5:9b", "smollm2:135m"}, cfg)
	var chat libRow
	for _, r := range rows {
		if r.Alias == "chat" {
			chat = r
		}
	}
	if !chat.Installed || !chat.Active {
		t.Fatalf("chat row: %+v", chat)
	}
}
