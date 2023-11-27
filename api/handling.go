package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/golang/gddo/httputil"
)

const (
	ContentTypePlainText = "text/plain"
	ContentTypeJSON      = "application/json"
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

func Render(w http.ResponseWriter, r *http.Request, response any) error {
	switch NegotiateContentType(r) {
	case ContentTypePlainText:
		render.PlainText(w, r, response.(string))
	case ContentTypeJSON:
		render.JSON(w, r, response)
	}
	return nil
}

// negotiateContentType parses "Accept:" header and returns preferred content type string.
func NegotiateContentType(r *http.Request) string {
	contentTypes := []string{
		ContentTypePlainText,
		ContentTypeJSON,
	}
	return httputil.NegotiateContentType(r, contentTypes, ContentTypePlainText)
}
