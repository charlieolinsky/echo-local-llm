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
		"c clear",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}

func TestClearLogsHidesVisibleLines(t *testing.T) {
	st := testStore(t)
	m := newModel(st, newController(st, ""))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)
	m.logText = "15:04:05  200  POST /v1/chat/completions  12ms  127.0.0.1  tiny"
	m.logSize = 64
	m.note("gateway started")
	m.redrawLogs()
	view := m.View()
	if !strings.Contains(view, "POST /v1/chat/completions") {
		t.Fatalf("expected log line before clear\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(model)
	view = m.View()
	if strings.Contains(view, "POST /v1/chat/completions") {
		t.Fatalf("log line still visible after clear\n%s", view)
	}
	if strings.Contains(view, "gateway started") {
		t.Fatalf("notice still visible after clear\n%s", view)
	}
	if m.logFloor != 64 {
		t.Fatalf("logFloor=%d want 64", m.logFloor)
	}
	if !strings.Contains(view, "No activity yet") {
		t.Fatalf("expected empty-state hint after clear\n%s", view)
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
