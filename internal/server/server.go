package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/charlesolinsky/local-llm/internal/activity"
	"github.com/charlesolinsky/local-llm/internal/auth"
	"github.com/charlesolinsky/local-llm/internal/config"
	"github.com/charlesolinsky/local-llm/internal/proxy"
)

// Server is the LAN-facing HTTP gateway.
type Server struct {
	cfg        *config.Config
	gw         *proxy.Gateway
	httpServer *http.Server
	quiet      bool
}

// Option configures New.
type Option func(*Server)

// Quiet suppresses listen banners and access logs (needed while a TUI owns the terminal).
func Quiet() Option {
	return func(s *Server) { s.quiet = true }
}

// New wires auth + routes for the gateway.
func New(cfg *config.Config, opts ...Option) (*Server, error) {
	gw, err := proxy.New(cfg)
	if err != nil {
		return nil, err
	}

	s := &Server{cfg: cfg, gw: gw}
	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", gw.Health)

	protected := auth.Bearer(cfg.APIKey)(http.HandlerFunc(gw.ServeV1))
	mux.Handle("/v1/", protected)
	mux.Handle("/v1", protected)

	s.httpServer = &http.Server{
		Addr:              cfg.Listen,
		Handler:           logging(mux, s.quiet),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}
	if s.quiet {
		s.httpServer.ErrorLog = log.New(io.Discard, "", 0)
	}
	return s, nil
}

// ListenAndServe starts the HTTP server (blocking).
func (s *Server) ListenAndServe() error {
	activity.Record(activity.Event{
		Kind:    "info",
		Message: fmt.Sprintf("listening on %s  ollama %s  models %s", s.cfg.Listen, s.cfg.OllamaBase, strings.Join(s.cfg.AliasNames(), ",")),
	})
	if !s.quiet {
		log.Printf("local-llm listening on http://%s", s.cfg.Listen)
		log.Printf("proxying to Ollama at %s (loopback only recommended)", s.cfg.OllamaBase)
		log.Printf("configured models: %v", s.cfg.AliasNames())
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	activity.Record(activity.Event{Kind: "info", Message: "shutting down"})
	return s.httpServer.Shutdown(ctx)
}

func logging(next http.Handler, quiet bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		model := peekModel(r)
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		dur := time.Since(start)

		activity.Record(activity.Event{
			Kind:   "req",
			Method: r.Method,
			Path:   r.URL.Path,
			Status: rw.status,
			Dur:    dur,
			Remote: stripPort(r.RemoteAddr),
			Model:  model,
		})
		if !quiet {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, dur.Round(time.Millisecond))
		}
	})
}

func peekModel(r *http.Request) string {
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return ""
	}
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(strings.ToLower(ct), "json") {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	var payload struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return payload.Model
}

func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher when the underlying writer supports it (SSE/streaming).
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Addr returns the configured listen address.
func (s *Server) Addr() string {
	return s.cfg.Listen
}

// String helper for status output.
func (s *Server) String() string {
	return fmt.Sprintf("listen=%s ollama=%s models=%d", s.cfg.Listen, s.cfg.OllamaBase, len(s.cfg.Models))
}
