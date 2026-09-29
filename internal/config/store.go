package config

import (
	"fmt"
	"os"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

// Store is a thread-safe config that can be saved and reloaded from disk.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

// Open loads path into a Store.
func Open(path string) (*Store, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, cfg: cfg}, nil
}

// Path returns the YAML path.
func (s *Store) Path() string { return s.path }

// Snapshot returns a copy of the current config.
func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cfg)
}

// Reload re-reads the YAML file (keeps the in-memory API key if the file omits it).
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.cfg.APIKey
	cfg, err := Load(s.path)
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		cfg.APIKey = key
	}
	s.cfg = cfg
	return nil
}

// Save writes the current config to disk.
func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return writeFile(s.path, s.cfg)
}

// SetActive persists the active alias.
func (s *Store) SetActive(alias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cfg.Models[alias]; !ok {
		return fmt.Errorf("unknown alias %q", alias)
	}
	s.cfg.Active = alias
	return writeFile(s.path, s.cfg)
}

// SetContextLength persists num_ctx.
func (s *Store) SetContextLength(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.ContextLength = n
	return writeFile(s.path, s.cfg)
}

// UpsertModel adds or updates an alias and saves.
func (s *Store) UpsertModel(alias, upstream string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Models == nil {
		s.cfg.Models = map[string]ModelEntry{}
	}
	prev := s.cfg.Models[alias]
	s.cfg.Models[alias] = ModelEntry{
		Upstream:   upstream,
		NumCtx:     prev.NumCtx,
		NumPredict: prev.NumPredict,
	}
	if s.cfg.Active == "" {
		s.cfg.Active = alias
	}
	return writeFile(s.path, s.cfg)
}

// RemoveModel drops an alias. If it was active, picks another.
func (s *Store) RemoveModel(alias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cfg.Models[alias]; !ok {
		return fmt.Errorf("unknown alias %q", alias)
	}
	if len(s.cfg.Models) == 1 {
		return fmt.Errorf("cannot remove the last model alias")
	}
	delete(s.cfg.Models, alias)
	if s.cfg.Active == alias {
		s.cfg.Active = firstAlias(s.cfg.Models)
	}
	return writeFile(s.path, s.cfg)
}

func firstAlias(m map[string]ModelEntry) string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func clone(c *Config) Config {
	out := *c
	out.Models = make(map[string]ModelEntry, len(c.Models))
	for k, v := range c.Models {
		out.Models[k] = v
	}
	return out
}

func writeFile(path string, cfg *Config) error {
	type file struct {
		Listen        string                `yaml:"listen"`
		OllamaBase    string                `yaml:"ollama_base"`
		Active        string                `yaml:"active"`
		ContextLength int                   `yaml:"context_length"`
		Models        map[string]ModelEntry `yaml:"models"`
	}
	doc := file{
		Listen:        cfg.Listen,
		OllamaBase:    cfg.OllamaBase,
		Active:        cfg.Active,
		ContextLength: cfg.ContextLength,
		Models:        cfg.Models,
	}
	data, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	header := "# local-llm gateway — edited by the TUI; keep .env for the API key\n"
	return os.WriteFile(path, append([]byte(header), data...), 0o644)
}
