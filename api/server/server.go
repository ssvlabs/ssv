package server

import (
	"net/http"
	"time"

	"github.com/bloxapp/ssv/api"
	"github.com/bloxapp/ssv/api/node"
	"github.com/go-chi/chi/v5"
)

type Server struct {
	addr string
	node *node.Handler
}

func New(addr string, nodeHandler *node.Handler) *Server {
	return &Server{
		addr: addr,
		node: nodeHandler,
	}
}

func (s *Server) Run() error {
	router := chi.NewRouter()

	router.Get("/v1/node/peers", api.Handler(s.node.Peers))

	server := &http.Server{
		Addr:         s.addr,
		Handler:      router,
		ReadTimeout:  12 * time.Second,
		WriteTimeout: 12 * time.Second,
	}
	return server.ListenAndServe()
}
