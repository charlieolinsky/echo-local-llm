package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	content := `
listen: "127.0.0.1:4000"
ollama_base: "http://127.0.0.1:11434"
models:
  tiny:
    upstream: "smollm2:135m"
  chat:
    upstream: "qwen3.5:9b"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvAPIKey, "test-secret-key")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != "127.0.0.1:4000" {
		t.Fatalf("listen: got %q", cfg.Listen)
	}

	up, ok := cfg.Resolve("tiny")
	if !ok || up != "smollm2:135m" {
		t.Fatalf("Resolve(tiny): %q %v", up, ok)
	}
	up, ok = cfg.Resolve("qwen3.5:9b")
	if !ok || up != "qwen3.5:9b" {
		t.Fatalf("Resolve(upstream): %q %v", up, ok)
	}
	if cfg.Active != "chat" {
		t.Fatalf("default active: %q", cfg.Active)
	}
	up, ok = cfg.Resolve("default")
	if !ok || up != "qwen3.5:9b" {
		t.Fatalf("Resolve(default): %q %v", up, ok)
	}
	if _, ok := cfg.Resolve("missing"); ok {
		t.Fatal("expected missing model to fail")
	}

	names := cfg.AliasNames()
	if len(names) != 2 || names[0] != "chat" || names[1] != "tiny" {
		t.Fatalf("AliasNames: %v", names)
	}
}

func TestLookupAliasLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	content := `
models:
  echo:
    upstream: "echo"
    num_ctx: 8192
    num_predict: 900
  chat:
    upstream: "qwen3.5:9b"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAPIKey, "test-secret-key")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Lookup("echo")
	if !ok || entry.Upstream != "echo" || entry.NumCtx != 8192 || entry.NumPredict != 900 {
		t.Fatalf("lookup echo: %+v ok=%v", entry, ok)
	}
	chat, ok := cfg.Lookup("chat")
	if !ok || chat.NumCtx != 0 || chat.NumPredict != 0 {
		t.Fatalf("lookup chat: %+v", chat)
	}
}

func TestUpsertKeepsAliasLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	content := `
models:
  echo:
    upstream: "echo"
    num_ctx: 8192
    num_predict: 900
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvAPIKey, "test-secret-key")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertModel("echo", "echo"); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := st2.Snapshot().Models["echo"]
	if entry.NumCtx != 8192 || entry.NumPredict != 900 {
		t.Fatalf("limits dropped on upsert: %+v", entry)
	}
}

func TestLoadRequiresAPIKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	_ = os.WriteFile(path, []byte(`models:
  tiny:
    upstream: "smollm2:135m"
`), 0o644)
	t.Setenv(EnvAPIKey, "")
	if _, err := Load(path); err == nil {
		t.Fatal("expected error without API key")
	}
}

func TestStoreActiveAndContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	_ = os.WriteFile(path, []byte(`
listen: "127.0.0.1:4000"
ollama_base: "http://127.0.0.1:11434"
models:
  tiny:
    upstream: "smollm2:135m"
  chat:
    upstream: "qwen3.5:9b"
`), 0o644)
	t.Setenv(EnvAPIKey, "test-secret-key")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetActive("tiny"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetContextLength(4096); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	snap := st2.Snapshot()
	if snap.Active != "tiny" || snap.ContextLength != 4096 {
		t.Fatalf("saved state: %+v", snap)
	}
}
