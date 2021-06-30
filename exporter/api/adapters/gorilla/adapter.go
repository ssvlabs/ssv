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
		logger := ga.logger.With(zap.String("cid", api.ConnectionID(conn)))
		if err != nil {
			logger.Error("could not upgrade connection", zap.Error(err))
			return
		}
		logger.Debug("new websocket connection")
		defer func() {
			logger.Debug("closing connection")
			err := conn.Close()
			if err != nil {
				logger.Error("could not close connection", zap.Error(err))
			}
		}()
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

// IsCloseError returns true if the error originate as part of some close procedure
func (ga *gorillaAdapter) IsCloseError(err error) bool {
	_, ok := err.(*websocket.CloseError)
	return ok
}
