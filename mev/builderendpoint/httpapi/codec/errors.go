package codec

import "fmt"

// UnsupportedContentTypeError is returned when the request Content-Type is not supported.
type UnsupportedContentTypeError struct {
	ContentType string
}

func (e UnsupportedContentTypeError) Error() string {
	if e.ContentType == "" {
		return "unsupported content type"
	}
	return fmt.Sprintf("unsupported content type %q", e.ContentType)
}
