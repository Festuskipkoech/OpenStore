package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, map[string]string{
		"error": message,
		"code":  code,
	})
}

// writeErrorWithCode writes a JSON error using a provided machine code.
func writeErrorWithCode(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}{Error: message, Code: code})
}
var errLimitExceeded = fmt.Errorf("size limit exceeded")

func (r *countingLimitedReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.bytesRead += int64(n)
	if r.bytesRead > r.limit {
		return n, errLimitExceeded
	}
	return n, err
}

func isLimitExceeded(err error) bool {
	return err == errLimitExceeded || strings.Contains(err.Error(), "size limit exceeded")
}

// headerWriter fills a fixed buffer with the first N bytes written to it and discards the rest.
// Used as the tee destination for magic byte capture — never allocates beyond the header buffer.
type headerWriter struct {
	buf    []byte
	filled *int
}

func (h *headerWriter) Write(p []byte) (int, error) {
	if needed := len(h.buf) - *h.filled; needed > 0 {
		n := copy(h.buf[*h.filled:], p)
		*h.filled += n
	}
	return len(p), nil
}

// mimeOnly strips Content-Type parameters: "image/jpeg; charset=utf-8" -> "image/jpeg".
func mimeOnly(ct string) string {
	if i := strings.Index(ct, ";"); i != -1 {
		return strings.TrimSpace(ct[:i])
	}
	return strings.TrimSpace(ct)
}
