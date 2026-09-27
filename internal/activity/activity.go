package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxRing = 500

// Event is one gateway activity line.
type Event struct {
	Time    time.Time
	Kind    string // "req", "info", "error"
	Method  string
	Path    string
	Status  int
	Dur     time.Duration
	Remote  string
	Model   string
	Message string
}

func (e Event) String() string {
	ts := e.Time.Format("15:04:05")
	switch e.Kind {
	case "req":
		model := e.Model
		if model == "" {
			model = "-"
		}
		remote := e.Remote
		if remote == "" {
			remote = "-"
		}
		return fmt.Sprintf("%s  %3d  %-4s %-24s  %7s  %-15s  %s",
			ts, e.Status, e.Method, trunc(e.Path, 24), e.Dur.Truncate(time.Millisecond), trunc(remote, 15), model)
	default:
		msg := e.Message
		if msg == "" {
			msg = e.Path
		}
		return fmt.Sprintf("%s  %-5s  %s", ts, e.Kind, msg)
	}
}

// Hub is a process-local ring buffer that also appends to a log file.
type Hub struct {
	mu      sync.Mutex
	events  []Event
	seq     uint64
	path    string
	reqs    int
	errors  int
}

var defaultHub = New(DefaultPath())

// Default returns the process-wide hub.
func Default() *Hub { return defaultHub }

// DefaultPath is ~/Library/Logs/local-llm.log on macOS, or ~/.local-llm/gateway.log elsewhere.
func DefaultPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		mac := filepath.Join(home, "Library", "Logs")
		if st, err := os.Stat(mac); err == nil && st.IsDir() {
			return filepath.Join(mac, "local-llm.log")
		}
		return filepath.Join(home, ".local-llm", "gateway.log")
	}
	return "local-llm.log"
}

// New creates a hub that mirrors events to path.
func New(path string) *Hub {
	return &Hub{path: path, events: make([]Event, 0, 64)}
}

// Record appends an event to the ring and log file.
func (h *Hub) Record(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	if e.Kind == "req" {
		h.reqs++
		if e.Status >= 400 {
			h.errors++
		}
	}
	h.events = append(h.events, e)
	if len(h.events) > maxRing {
		h.events = h.events[len(h.events)-maxRing:]
	}
	if h.path != "" {
		_ = appendFile(h.path, e.String()+"\n")
	}
}

// Seq is a monotonically increasing write counter.
func (h *Hub) Seq() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seq
}

// Stats returns request and error counts for this process.
func (h *Hub) Stats() (reqs, errs int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reqs, h.errors
}

// Recent returns up to n latest events, oldest first.
func (h *Hub) Recent(n int) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n <= 0 || n > len(h.events) {
		n = len(h.events)
	}
	out := make([]Event, n)
	copy(out, h.events[len(h.events)-n:])
	return out
}

// Record is a convenience for the default hub.
func Record(e Event) { defaultHub.Record(e) }

func appendFile(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
