package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/charlesolinsky/local-llm/internal/config"
)

// Gateway reverse-proxies OpenAI-compatible /v1 traffic to Ollama,
// rewriting configured model aliases to upstream tags.
type Gateway struct {
	store  *config.Store
	proxy  *httputil.ReverseProxy
	client *http.Client
}

func (g *Gateway) cfg() config.Config {
	if g.store == nil {
		return config.Config{}
	}
	_ = g.store.Reload()
	return g.store.Snapshot()
}

// New builds a Gateway pointing at the store's Ollama base.
func New(store *config.Store) (*Gateway, error) {
	snap := store.Snapshot()
	target, err := url.Parse(snap.OllamaBase)
	if err != nil {
		return nil, fmt.Errorf("parse ollama_base: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.Header.Set("Host", target.Host)
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"ollama upstream unavailable","type":"server_error","code":"upstream_error"}}`))
	}
	rp.FlushInterval = 50 * time.Millisecond

	return &Gateway{
		store: store,
		proxy: rp,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}, nil
}

// Health reports whether Ollama is reachable.
func (g *Gateway) Health(w http.ResponseWriter, r *http.Request) {
	ollamaOK := false
	cfg := g.cfg()
	resp, err := g.client.Get(cfg.OllamaBase + "/api/tags")
	if err == nil {
		_ = resp.Body.Close()
		ollamaOK = resp.StatusCode >= 200 && resp.StatusCode < 300
	}

	status := http.StatusOK
	state := "ok"
	if !ollamaOK {
		status = http.StatusServiceUnavailable
		state = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"status":%q,"ollama":%t,"listen":%q,"models":%d,"active":%q,"context_length":%d}`,
		state, ollamaOK, cfg.Listen, len(cfg.Models), cfg.Active, cfg.ContextLength)
}

// ListModels returns OpenAI-style /v1/models for configured aliases only.
func (g *Gateway) ListModels(w http.ResponseWriter, r *http.Request) {
	type modelObj struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	type listResp struct {
		Object string     `json:"object"`
		Data   []modelObj `json:"data"`
	}

	cfg := g.cfg()
	now := time.Now().Unix()
	out := listResp{Object: "list", Data: make([]modelObj, 0, len(cfg.Models)+1)}
	out.Data = append(out.Data, modelObj{ID: "default", Object: "model", Created: now, OwnedBy: "local-llm"})
	for name := range cfg.Models {
		out.Data = append(out.Data, modelObj{
			ID:      name,
			Object:  "model",
			Created: now,
			OwnedBy: "local-llm",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ServeV1 proxies /v1/* to Ollama, rewriting the model field when present.
func (g *Gateway) ServeV1(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && (r.URL.Path == "/v1/models" || r.URL.Path == "/v1/models/") {
		g.ListModels(w, r)
		return
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/v1/chat/completions/") {
		g.handleChatCompletions(w, r)
		return
	}

	if err := g.rewriteModel(r); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		msg, _ := json.Marshal(map[string]any{
			"error": map[string]string{
				"message": err.Error(),
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		_, _ = w.Write(msg)
		return
	}

	g.proxy.ServeHTTP(w, r)
}

func (g *Gateway) rewriteModel(r *http.Request) error {
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}

	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(strings.ToLower(ct), "json") {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20)) // 32 MiB cap
	_ = r.Body.Close()
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		// Not JSON we understand — pass through unchanged.
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return nil
	}

	rawModel, ok := payload["model"]
	if !ok {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return nil
	}

	var modelName string
	if err := json.Unmarshal(rawModel, &modelName); err != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return nil
	}

	cfg := g.cfg()
	upstream, ok := cfg.Resolve(modelName)
	if !ok {
		aliases := strings.Join(cfg.AliasNames(), ", ")
		return fmt.Errorf("unknown model %q; configured aliases: %s", modelName, aliases)
	}

	encoded, err := json.Marshal(upstream)
	if err != nil {
		return err
	}
	payload["model"] = encoded

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	r.Body = io.NopCloser(bytes.NewReader(rewritten))
	r.ContentLength = int64(len(rewritten))
	r.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	return nil
}
