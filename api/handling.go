package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/golang/gddo/httputil"
)

const (
	contentTypePlainText = "text/plain"
	contentTypeJSON      = "application/json"
)

type HandlerFunc func(http.ResponseWriter, *http.Request) error

func Handler(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			//nolint:all
			switch e := err.(type) {
			case render.Renderer:
				render.Render(w, r, e)
			default:
				render.Render(w, r, Error(err))
			}
		}
	}
}

// Render negotiates the content type and renders the response, defaulting to JSON.
// Response must implement fmt.Stringer to be rendered as plain text.
func Render(w http.ResponseWriter, r *http.Request, response any) error {
	// Negotiate content type, defaulting to JSON.
	contentType := httputil.NegotiateContentType(
		r,
		[]string{contentTypePlainText, contentTypeJSON},
		contentTypeJSON,
	)

	switch contentType {
	case contentTypePlainText:
		// Try rendering as a string, otherwise fallback to JSON.
		if stringer, ok := response.(fmt.Stringer); ok {
			render.PlainText(w, r, stringer.String())
			return nil
		}
		fallthrough
	default:
		render.JSON(w, r, response)
		return nil
	}
}
