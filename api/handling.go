package api

import (
	"github.com/go-chi/render"
	"net/http"
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
