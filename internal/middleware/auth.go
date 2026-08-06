package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func Auth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			incoming := extractBearer(r.Header.Get("Authorization"))
			if incoming == "" || !constantTimeEqual(apiKey, incoming ) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid or missing API key","code":"unauthorized"}`))
				return 
			}
			next.ServeHTTP(w, r)
		})
	}
}

func extractBearer(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return  ""
	}
	return  strings.TrimSpace(header[len(prefix):])
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}