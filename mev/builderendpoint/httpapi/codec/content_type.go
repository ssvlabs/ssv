package codec

import "strings"

// NormalizeContentType returns the Content-Type without any parameters.
// If the given content type is empty it defaults to "application/json" for backwards compatibility.
func NormalizeContentType(contentType string) string {
	if contentType == "" {
		// Backwards-compatible default.
		contentType = "application/json"
	}
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = contentType[:idx]
	}
	return strings.TrimSpace(contentType)
}
