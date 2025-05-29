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
			//nolint:errorlint
			// errors.As would be incorrect here since a renderer.Renderer
			// wrapped inside another error should error, not render.
			switch e := err.(type) {
			case render.Renderer:
				if err := render.Render(w, r, e); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			default:
				if err := render.Render(w, r, Error(err)); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
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
