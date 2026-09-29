package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charlesolinsky/local-llm/internal/config"
)

func testGateway(t *testing.T, ollamaBase, modelsYAML string) *Gateway {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "models.yaml")
	content := fmt.Sprintf("listen: \"127.0.0.1:0\"\nollama_base: %q\ncontext_length: 32768\nmodels:\n%s\n", ollamaBase, modelsYAML)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvAPIKey, "test-secret-key")
	st, err := config.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(st)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func chatRequest(t *testing.T, model string, maxTokens *int, responseFormat any) *http.Request {
	t.Helper()
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	if maxTokens != nil {
		payload["max_tokens"] = *maxTokens
	}
	if responseFormat != nil {
		payload["response_format"] = responseFormat
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func ollamaOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"{\"ok\":true}"},"done":true,"done_reason":"stop"}`)
}

func TestChatForwardsJSONModeAndAliasLimits(t *testing.T) {
	var got ollamaChatRequest
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeErr = json.NewDecoder(r.Body).Decode(&got)
		ollamaOK(w)
	}))
	defer srv.Close()

	g := testGateway(t, srv.URL, `
  echo:
    upstream: "echo"
    num_ctx: 8192
    num_predict: 900
  chat:
    upstream: "qwen3.5:9b"
`)
	maxTokens := 400
	rec := httptest.NewRecorder()
	req := chatRequest(t, "echo", &maxTokens, map[string]string{"type": "json_object"})
	g.handleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if got.Model != "echo" {
		t.Fatalf("upstream model %q", got.Model)
	}
	if got.Format != "json" {
		t.Fatalf("format %#v", got.Format)
	}
	if got.Options["num_ctx"] != float64(8192) {
		t.Fatalf("num_ctx %#v", got.Options["num_ctx"])
	}
	if got.Options["num_predict"] != float64(400) {
		t.Fatalf("max_tokens should win: %#v", got.Options["num_predict"])
	}

	rec = httptest.NewRecorder()
	req = chatRequest(t, "echo", nil, map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name": "card",
			"schema": map[string]any{
				"type": "object",
			},
		},
	})
	g.handleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("schema status %d %s", rec.Code, rec.Body.String())
	}
	format, ok := got.Format.(map[string]any)
	if !ok || format["type"] != "object" {
		t.Fatalf("schema format %#v", got.Format)
	}
	if got.Options["num_predict"] != float64(900) {
		t.Fatalf("alias num_predict %#v", got.Options["num_predict"])
	}
}

func TestChatRejectsOverlap(t *testing.T) {
	var upstream atomic.Int32
	started := make(chan struct{})
	hold := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		once.Do(func() { close(started) })
		<-hold
		ollamaOK(w)
	}))
	defer srv.Close()

	g := testGateway(t, srv.URL, `
  chat:
    upstream: "qwen3.5:9b"
`)

	firstDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		g.handleChatCompletions(rec, chatRequest(t, "chat", nil, nil))
		firstDone <- rec.Code
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach upstream")
	}

	rec := httptest.NewRecorder()
	g.handleChatCompletions(rec, chatRequest(t, "chat", nil, nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("overlap status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	var errBody struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Error.Type != "rate_limit_error" {
		t.Fatalf("error type %q", errBody.Error.Type)
	}

	close(hold)
	select {
	case code := <-firstDone:
		if code != http.StatusOK {
			t.Fatalf("first status %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}
	if n := upstream.Load(); n != 1 {
		t.Fatalf("upstream calls %d", n)
	}
}

func TestChatCancelStopsUpstream(t *testing.T) {
	hit := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ollama reads the JSON body before generating. The server only
		// notices a client hangup after that read finishes.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		once.Do(func() { close(hit) })
		<-r.Context().Done()
	}))
	defer srv.Close()

	g := testGateway(t, srv.URL, `
  chat:
    upstream: "qwen3.5:9b"
`)
	ctx, cancel := context.WithCancel(context.Background())
	req := chatRequest(t, "chat", nil, nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		g.handleChatCompletions(httptest.NewRecorder(), req)
		close(done)
	}()
	select {
	case <-hit:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not called")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler kept running after client cancel")
	}
}
