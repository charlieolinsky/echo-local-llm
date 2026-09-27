package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LibraryEntry is an installable model shown in the TUI catalog.
type LibraryEntry struct {
	Alias string `yaml:"alias"`
	Tag   string `yaml:"tag"`
	Size  string `yaml:"size"`
	Fit   string `yaml:"fit"` // easy | medium
	Role  string `yaml:"role"`
}

// DefaultLibrary is the built-in M4 16GB catalog (not installed until pulled).
func DefaultLibrary() []LibraryEntry {
	return []LibraryEntry{
		{Alias: "tiny", Tag: "smollm2:135m", Size: "270 MB", Fit: "easy", Role: "Smoke test"},
		{Alias: "fast", Tag: "llama3.2:3b", Size: "~2 GB", Fit: "easy", Role: "Instant answers"},
		{Alias: "small", Tag: "qwen3.5:4b", Size: "~3.5 GB", Fit: "easy", Role: "Small Qwen helper"},
		{Alias: "chat", Tag: "qwen3.5:9b", Size: "6.6 GB", Fit: "easy", Role: "Daily driver — summary & reasoning"},
		{Alias: "code", Tag: "qwen2.5-coder:7b", Size: "~4.7 GB", Fit: "easy", Role: "Coding"},
		{Alias: "llama", Tag: "llama3.1:8b", Size: "~4.9 GB", Fit: "easy", Role: "General Llama 3.1"},
		{Alias: "gemma", Tag: "gemma2:9b", Size: "~5.5 GB", Fit: "easy", Role: "Gemma 2 writing"},
	}
}

// LoadLibrary reads config/library.yaml next to models.yaml. Falls back to DefaultLibrary.
func LoadLibrary(modelsPath string) []LibraryEntry {
	dir := filepath.Dir(modelsPath)
	path := filepath.Join(dir, "library.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultLibrary()
	}
	var doc struct {
		Models []LibraryEntry `yaml:"models"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Models) == 0 {
		return DefaultLibrary()
	}
	return doc.Models
}
