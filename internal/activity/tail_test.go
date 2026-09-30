package activity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailFileAndReadSince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.log")
	first := "line-one\nline-two\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}
	tail, err := TailFile(path, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if tail != first {
		t.Fatalf("tail: %q", tail)
	}
	floor := FileSize(path)
	second := "line-three\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(second); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	since, err := ReadSince(path, floor, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if since != second {
		t.Fatalf("ReadSince: got %q want %q", since, second)
	}
	empty, err := ReadSince(path, FileSize(path), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if empty != "" {
		t.Fatalf("expected empty after floor at EOF, got %q", empty)
	}
}

func TestReadSinceAfterTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("old\n", 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	floor := FileSize(path)
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSince(path, floor, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fresh\n" {
		t.Fatalf("after truncate: %q", got)
	}
}
