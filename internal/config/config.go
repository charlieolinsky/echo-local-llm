package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModelEntry maps a public alias to an Ollama upstream model tag.
type ModelEntry struct {
	Upstream string `yaml:"upstream"`
}

// Config is the gateway configuration loaded from YAML + environment.
type Config struct {
	Listen     string                `yaml:"listen"`
	OllamaBase string                `yaml:"ollama_base"`
	APIKey     string                `yaml:"api_key"`
	Models     map[string]ModelEntry `yaml:"models"`
}

const (
	DefaultListen     = "0.0.0.0:4000"
	DefaultOllamaBase = "http://127.0.0.1:11434"
	EnvAPIKey         = "LOCAL_LLM_API_KEY"
)

// Load reads a YAML config file and applies environment overrides.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
	if cfg.OllamaBase == "" {
		cfg.OllamaBase = DefaultOllamaBase
	}
	cfg.OllamaBase = strings.TrimRight(cfg.OllamaBase, "/")

	if envKey := os.Getenv(EnvAPIKey); envKey != "" {
		cfg.APIKey = envKey
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%s is required (set in environment or api_key in config)", EnvAPIKey)
	}
	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("config %s: at least one model alias is required under models:", path)
	}
	for name, entry := range cfg.Models {
		if strings.TrimSpace(entry.Upstream) == "" {
			return nil, fmt.Errorf("config %s: model %q missing upstream", path, name)
		}
	}

	return cfg, nil
}

// Resolve returns the Ollama upstream tag for a client-facing model name.
// Accepts either a configured alias or a direct upstream tag that appears in the config.
func (c *Config) Resolve(name string) (string, bool) {
	if entry, ok := c.Models[name]; ok {
		return entry.Upstream, true
	}
	for _, entry := range c.Models {
		if entry.Upstream == name {
			return entry.Upstream, true
		}
	}
	return "", false
}

// AliasNames returns configured alias names in sorted order.
func (c *Config) AliasNames() []string {
	names := make([]string, 0, len(c.Models))
	for name := range c.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
