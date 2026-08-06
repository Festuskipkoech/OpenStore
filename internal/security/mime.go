package security

// IsAllowedMIME reports whether mimeType is present in allowedList.
// Comparison is exact — no wildcard or prefix matching.
func IsAllowedMIME(mimeType string, allowedList []string) bool {
	for _, allowed := range allowedList {
		if mimeType == allowed {
			return true
		}
	}
	return false
}