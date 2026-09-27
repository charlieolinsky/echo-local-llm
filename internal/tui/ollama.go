package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charlesolinsky/local-llm/internal/config"
)

func ollamaTagsURL(base string) string {
	return strings.TrimRight(base, "/") + "/api/tags"
}

func listOllamaTags(base string) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(ollamaTagsURL(base))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out, nil
}

func tagInstalled(tag string, installed []string) bool {
	for _, name := range installed {
		if name == tag {
			return true
		}
		if strings.HasPrefix(name, tag) || strings.HasPrefix(tag, name) {
			return true
		}
	}
	return false
}

func ollamaPull(tag string) error {
	cmd := exec.Command("ollama", "pull", tag)
	cmd.Env = append(os.Environ(), "OLLAMA_HOST=127.0.0.1:11434")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("pull %s: %s", tag, truncate(msg, 160))
	}
	return nil
}

func ollamaRemove(tag string) error {
	cmd := exec.Command("ollama", "rm", tag)
	cmd.Env = append(os.Environ(), "OLLAMA_HOST=127.0.0.1:11434")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("remove %s: %s", tag, truncate(msg, 160))
	}
	return nil
}

type libRow struct {
	Alias     string
	Tag       string
	Size      string
	Fit       string
	Role      string
	Installed bool
	Active    bool
}

func buildLibraryRows(lib []config.LibraryEntry, installed []string, cfg config.Config) []libRow {
	seen := map[string]bool{}
	rows := make([]libRow, 0, len(lib)+4)
	for _, e := range lib {
		rows = append(rows, libRow{
			Alias:     e.Alias,
			Tag:       e.Tag,
			Size:      e.Size,
			Fit:       e.Fit,
			Role:      e.Role,
			Installed: tagInstalled(e.Tag, installed),
			Active:    cfg.Active == e.Alias,
		})
		seen[e.Alias] = true
		seen[e.Tag] = true
	}
	// Aliases in config that aren't in the catalog (e.g. summarize).
	for _, alias := range cfg.AliasNames() {
		if seen[alias] {
			continue
		}
		up := cfg.Models[alias].Upstream
		rows = append(rows, libRow{
			Alias:     alias,
			Tag:       up,
			Size:      "local",
			Fit:       "easy",
			Role:      "Custom alias",
			Installed: tagInstalled(up, installed),
			Active:    cfg.Active == alias,
		})
	}
	return rows
}

func nextContext(cur, dir int) int {
	choices := config.ContextChoices
	idx := 0
	for i, n := range choices {
		if n == cur {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 {
		idx = len(choices) - 1
	}
	if idx >= len(choices) {
		idx = 0
	}
	return choices[idx]
}
