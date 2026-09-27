package activity

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordAndRecent(t *testing.T) {
	dir := t.TempDir()
	h := New(filepath.Join(dir, "gateway.log"))
	h.Record(Event{Kind: "info", Message: "listening", Time: time.Now()})
	h.Record(Event{
		Kind:   "req",
		Method: "POST",
		Path:   "/v1/chat/completions",
		Status: 200,
		Dur:    time.Millisecond,
		Model:  "tiny",
		Remote: "127.0.0.1",
	})
	got := h.Recent(10)
	if len(got) != 2 {
		t.Fatalf("Recent: %d", len(got))
	}
	if h.Seq() != 2 {
		t.Fatalf("seq: %d", h.Seq())
	}
	reqs, errs := h.Stats()
	if reqs != 1 || errs != 0 {
		t.Fatalf("stats %d %d", reqs, errs)
	}
	line := got[1].String()
	if !strings.Contains(line, "POST") || !strings.Contains(line, "tiny") {
		t.Fatalf("line: %s", line)
	}
}
