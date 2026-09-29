package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// handleChatCompletions serves OpenAI /v1/chat/completions via Ollama's native
// /api/chat so we can disable Qwen "thinking" (OpenAI-compat ignores think:false).
func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	_ = r.Body.Close()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "could not read body")
		return
	}

	var req openaiChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body")
		return
	}
	cfg := g.cfg()
	if req.Model == "" {
		req.Model = "default"
	}

	entry, ok := cfg.Lookup(req.Model)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "model_not_found",
			fmt.Sprintf("unknown model %q; configured aliases: %s", req.Model, strings.Join(cfg.AliasNames(), ", ")))
		return
	}

	if !g.acquireChat() {
		w.Header().Set("Retry-After", "2")
		writeAPIError(w, http.StatusTooManyRequests, "rate_limit_error", "a completion is already running")
		return
	}
	defer g.releaseChat()

	think := false
	if req.Think != nil {
		think = *req.Think
	}

	ollamaReq := ollamaChatRequest{
		Model:    entry.Upstream,
		Messages: req.Messages,
		Stream:   req.Stream,
		Think:    &think,
		Format:   ollamaFormat(req.ResponseFormat),
		Options:  map[string]any{},
	}
	numCtx := cfg.ContextLength
	if entry.NumCtx > 0 {
		numCtx = entry.NumCtx
	}
	if numCtx > 0 {
		ollamaReq.Options["num_ctx"] = numCtx
	}
	if req.Temperature != nil {
		ollamaReq.Options["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		ollamaReq.Options["num_predict"] = *req.MaxTokens
	} else if entry.NumPredict > 0 {
		ollamaReq.Options["num_predict"] = entry.NumPredict
	}
	if req.TopP != nil {
		ollamaReq.Options["top_p"] = *req.TopP
	}

	raw, err := json.Marshal(ollamaReq)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "encode failed")
		return
	}

	if req.Stream {
		g.streamOllamaChat(w, r, raw, req.Model, cfg.OllamaBase)
		return
	}

	resp, err := postOllama(r.Context(), cfg.OllamaBase, raw, 10*time.Minute)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "ollama unavailable")
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "read upstream failed")
		return
	}
	if resp.StatusCode >= 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}

	var ollamaResp ollamaChatResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "decode upstream failed")
		return
	}

	out := openaiChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()%1_000_000),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []openaiChoice{{
			Index: 0,
			Message: openaiMessage{
				Role:    ollamaResp.Message.Role,
				Content: ollamaResp.Message.Content,
			},
			FinishReason: mapDoneReason(ollamaResp.DoneReason),
		}},
		Usage: openaiUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}
	if out.Choices[0].Message.Role == "" {
		out.Choices[0].Message.Role = "assistant"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (g *Gateway) streamOllamaChat(w http.ResponseWriter, r *http.Request, raw []byte, alias, ollamaBase string) {
	resp, err := postOllama(r.Context(), ollamaBase, raw, 0)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_error", "ollama unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	dec := json.NewDecoder(resp.Body)
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()%1_000_000)
	created := time.Now().Unix()
	for {
		var chunk ollamaChatResponse
		if err := dec.Decode(&chunk); err != nil {
			break
		}
		delta := openaiStreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   alias,
			Choices: []openaiStreamChoice{{
				Index: 0,
				Delta: openaiMessage{
					Role:    chunk.Message.Role,
					Content: chunk.Message.Content,
				},
			}},
		}
		if chunk.Done {
			delta.Choices[0].FinishReason = mapDoneReason(chunk.DoneReason)
		}
		payload, _ := json.Marshal(delta)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		flusher.Flush()
		if chunk.Done {
			break
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
}

type openaiChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	Stream         bool            `json:"stream"`
	Temperature    *float64        `json:"temperature"`
	MaxTokens      *int            `json:"max_tokens"`
	TopP           *float64        `json:"top_p"`
	Think          *bool           `json:"think"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openaiChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openaiChoice `json:"choices"`
	Usage   openaiUsage    `json:"usage"`
}

type openaiChoice struct {
	Index        int           `json:"index"`
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openaiStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openaiStreamChoice `json:"choices"`
}

type openaiStreamChoice struct {
	Index        int           `json:"index"`
	Delta        openaiMessage `json:"delta"`
	FinishReason string        `json:"finish_reason,omitempty"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Think    *bool           `json:"think,omitempty"`
	Format   any             `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

// postOllama calls Ollama and stops when ctx is canceled, so a client that
// times out does not leave a generation running.
func postOllama(ctx context.Context, base string, raw []byte, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

// ollamaFormat maps OpenAI response_format onto Ollama's format field.
// Unknown shapes are ignored so older clients keep working.
func ollamaFormat(raw json.RawMessage) any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var meta struct {
		Type       string          `json:"type"`
		JSONSchema json.RawMessage `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil
	}
	switch meta.Type {
	case "json_object":
		return "json"
	case "json_schema":
		if schema := schemaObject(meta.JSONSchema); schema != nil {
			return schema
		}
		return "json"
	default:
		return nil
	}
}

func schemaObject(raw json.RawMessage) any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var wrapped struct {
		Schema json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(bytes.TrimSpace(wrapped.Schema)) > 0 {
		var schema any
		if json.Unmarshal(wrapped.Schema, &schema) == nil {
			return schema
		}
	}
	var direct any
	if json.Unmarshal(raw, &direct) == nil {
		if _, ok := direct.(map[string]any); ok {
			return direct
		}
	}
	return nil
}

type ollamaChatResponse struct {
	Model           string        `json:"model"`
	Message         openaiMessage `json:"message"`
	Done            bool          `json:"done"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

func mapDoneReason(r string) string {
	switch r {
	case "stop", "":
		return "stop"
	case "length":
		return "length"
	default:
		return r
	}
}

func writeAPIError(w http.ResponseWriter, code int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": msg,
			"type":    errType,
			"code":    errType,
		},
	})
}
