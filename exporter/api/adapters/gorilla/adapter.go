package gorilla

import (
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net/http"
)

type gorillaAdapter struct {
	logger *zap.Logger
}

// TODO: check buffer sizes
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// NewGorillaAdapter creates a new instance of the adapter
func NewGorillaAdapter(logger *zap.Logger) api.WebSocketAdapter {
	wsa := gorillaAdapter{
		logger: logger.With(zap.String("component", "WebSocketAdapter")),
	}
	return &wsa
}

func (ga *gorillaAdapter) RegisterHandler(mux *http.ServeMux, endPoint string, handler api.EndPointHandler) {
	mux.HandleFunc(endPoint, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, w.Header())
		if err != nil {
			ga.logger.Error("could not upgrade connection", zap.Error(err))
			return
		}
		defer conn.Close()
		handler(conn)
	})
}

func (ga *gorillaAdapter) Send(conn api.Connection, v interface{}) error {
	c, ok := conn.(*websocket.Conn)
	if !ok {
		return errors.Errorf("connection object does not fit to adapter")
	}
	return c.WriteJSON(v)
}

func (ga *gorillaAdapter) Receive(conn api.Connection, v interface{}) error {
	c, ok := conn.(*websocket.Conn)
	if !ok {
		return errors.Errorf("connection object does not fit to adapter")
	}
	return c.ReadJSON(v)
}
