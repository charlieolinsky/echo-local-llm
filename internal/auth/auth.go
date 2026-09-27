package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Bearer returns middleware that requires Authorization: Bearer <apiKey>.
func Bearer(apiKey string) func(http.Handler) http.Handler {
	expected := []byte(apiKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := bearerToken(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", `Bearer realm="local-llm"`)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid or missing API key","type":"invalid_request_error","code":"invalid_api_key"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
