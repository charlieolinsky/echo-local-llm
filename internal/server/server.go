package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/charlesolinsky/local-llm/internal/auth"
	"github.com/charlesolinsky/local-llm/internal/config"
	"github.com/charlesolinsky/local-llm/internal/proxy"
)

// Server is the LAN-facing HTTP gateway.
type Server struct {
	cfg *config.Config
	gw  *proxy.Gateway
	httpServer *http.Server
}

// New wires auth + routes for the gateway.
func New(cfg *config.Config) (*Server, error) {
	gw, err := proxy.New(cfg)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", gw.Health)

	protected := auth.Bearer(cfg.APIKey)(http.HandlerFunc(gw.ServeV1))
	mux.Handle("/v1/", protected)
	mux.Handle("/v1", protected)

	s := &Server{
		cfg: cfg,
		gw:  gw,
		httpServer: &http.Server{
			Addr:              cfg.Listen,
			Handler:           logging(mux),
			ReadHeaderTimeout: 10 * time.Second,
			// Long-lived for streaming completions.
			ReadTimeout:  0,
			WriteTimeout: 0,
			IdleTimeout:  120 * time.Second,
		},
	}
	return s, nil
}

// ListenAndServe starts the HTTP server (blocking).
func (s *Server) ListenAndServe() error {
	log.Printf("local-llm listening on http://%s", s.cfg.Listen)
	log.Printf("proxying to Ollama at %s (loopback only recommended)", s.cfg.OllamaBase)
	log.Printf("configured models: %v", s.cfg.AliasNames())
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start).Round(time.Millisecond))
	})
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
