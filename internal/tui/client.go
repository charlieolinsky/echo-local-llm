package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/charlesolinsky/local-llm/internal/config"
)

type healthInfo struct {
	Status string `json:"status"`
	Ollama bool   `json:"ollama"`
	Listen string `json:"listen"`
	Models int    `json:"models"`
}

type chatMessage struct {
	Role    string
	Content string
}

type apiClient struct {
	cfg    *config.Config
	client *http.Client
	base   string // always hit loopback for local control
}

func newAPIClient(cfg *config.Config) *apiClient {
	host := cfg.Listen
	if strings.HasPrefix(host, "0.0.0.0") {
		host = "127.0.0.1" + strings.TrimPrefix(host, "0.0.0.0")
	} else if strings.HasPrefix(host, "[::]") {
		host = "127.0.0.1" + strings.TrimPrefix(host, "[::]")
	} else if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return &apiClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		base: "http://" + host,
	}
}

func (c *apiClient) health() (healthInfo, error) {
	var h healthInfo
	req, err := http.NewRequest(http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return h, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &h); err != nil {
		return h, fmt.Errorf("health decode: %w (%s)", err, truncate(string(body), 80))
	}
	if resp.StatusCode >= 300 && h.Status == "" {
		h.Status = "degraded"
	}
	return h, nil
}

func (c *apiClient) chat(model, prompt string) (string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.base+"/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("%s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return parsed.Choices[0].Message.Content, nil
}

func lanIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			s := ip.String()
			// Prefer common private LAN ranges.
			if strings.HasPrefix(s, "192.168.") || strings.HasPrefix(s, "10.") || strings.HasPrefix(s, "172.") {
				return s
			}
			if fallback == "" {
				fallback = s
			}
		}
	}
	return fallback
}

func listenPort(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[i+1:]
	}
	return "4000"
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("•", len(key))
	}
	return strings.Repeat("•", len(key)-4) + key[len(key)-4:]
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
