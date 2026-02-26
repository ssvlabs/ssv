package codec

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	MediaTypeJSON = "application/json"
	MediaTypeSSZ  = "application/octet-stream"
)

type NotAcceptableError struct {
	Accept string
}

func (e NotAcceptableError) Error() string {
	if e.Accept == "" {
		return "not acceptable"
	}
	return fmt.Sprintf("not acceptable: %q", e.Accept)
}

// PreferredResponseContentType negotiates the response content-type based on the HTTP Accept header.
// It supports JSON and SSZ ("application/octet-stream") and returns an error if none are acceptable.
func PreferredResponseContentType(acceptHeader string) (string, error) {
	if strings.TrimSpace(acceptHeader) == "" {
		// Default to JSON for backwards compatibility.
		return MediaTypeJSON, nil
	}

	type candidate struct {
		media string
		q     float64
	}

	best := candidate{media: "", q: 0}

	for _, part := range strings.Split(acceptHeader, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		media := part
		q := 1.0

		if semi := strings.Index(media, ";"); semi >= 0 {
			media = strings.TrimSpace(media[:semi])
			params := strings.Split(part[semi+1:], ";")
			for _, p := range params {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(strings.ToLower(p), "q=") {
					raw := strings.TrimSpace(p[2:])
					if v, err := strconv.ParseFloat(raw, 64); err == nil {
						q = v
					}
				}
			}
		}

		media = strings.ToLower(strings.TrimSpace(media))
		if media == "*/*" {
			media = MediaTypeJSON
		}

		if media != MediaTypeJSON && media != MediaTypeSSZ {
			continue
		}
		if q <= 0 {
			continue
		}
		if q > best.q {
			best = candidate{media: media, q: q}
		}
	}

	if best.media == "" {
		return "", NotAcceptableError{Accept: acceptHeader}
	}

	return best.media, nil
}
