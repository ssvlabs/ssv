package api

import (
	"context"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/gorilla/websocket"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
	"net/http"
	"time"
)

const (
	sendTimeout = 3 * time.Second
)

// WebSocketServer is responsible for managing all
type WebSocketServer interface {
	Start(addr string) error
	BroadcastFeed() *event.Feed
	UseQueryHandler(handler QueryMessageHandler)
}

// wsServer is an implementation of WebSocketServer
type wsServer struct {
	ctx context.Context

	logger *zap.Logger

	handler QueryMessageHandler

	broadcaster Broadcaster

	router *http.ServeMux
	// out is a subject for writing messages
	out      *event.Feed
	withPing bool
}

// NewWsServer creates a new instance
func NewWsServer(ctx context.Context, logger *zap.Logger, handler QueryMessageHandler, mux *http.ServeMux, withPing bool) WebSocketServer {
	ws := wsServer{
		ctx:         ctx,
		logger:      logger.With(zap.String("component", "exporter/api/server")),
		handler:     handler,
		router:      mux,
		broadcaster: newBroadcaster(logger),
		out:         new(event.Feed),
		withPing:    withPing,
	}
	return &ws
}

func (ws *wsServer) UseQueryHandler(handler QueryMessageHandler) {
	ws.handler = handler
}

// Start starts the websocket server and the broadcaster
func (ws *wsServer) Start(addr string) error {
	ws.RegisterHandler("/query", ws.handleQuery)
	ws.RegisterHandler("/stream", ws.handleStream)

	go func() {
		if err := ws.broadcaster.FromFeed(ws.out); err != nil {
			ws.logger.Debug("failed to pull messages from feed")
		}
	}()
	ws.logger.Info("starting websocket server",
		zap.String("addr", addr),
		zap.Strings("endPoints", []string{"/query", "/stream"}))

	err := http.ListenAndServe(addr, ws.router)
	if err != nil {
		ws.logger.Warn("could not start http server", zap.Error(err))
	}
	return err
}

// BroadcastFeed returns the feed for stream messages
func (ws *wsServer) BroadcastFeed() *event.Feed {
	return ws.out
}

// RegisterHandler registers an end point
func (ws *wsServer) RegisterHandler(endPoint string, handler func(conn *websocket.Conn)) {
	ws.router.HandleFunc(endPoint, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, w.Header())
		logger := ws.logger.With(zap.String("remote addr", conn.RemoteAddr().String()))
		if err != nil {
			ws.logger.Error("could not upgrade connection", zap.Error(err))
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

// handleQuery receives query message and respond async
func (ws *wsServer) handleQuery(conn *websocket.Conn) {
	if ws.handler == nil {
		return
	}
	cid := ConnectionID(conn)
	logger := ws.logger.With(zap.String("cid", cid))
	logger.Debug("handles query requests")

	for {
		if ws.ctx.Err() != nil {
			logger.Debug("context was done")
		}
		var incoming Message
		var nm NetworkMessage
		err := conn.ReadJSON(&incoming)
		if err != nil {
			if isCloseError(err) {
				logger.Debug("failed to read message as the connection was closed", zap.Error(err))
				return
			}
			ws.logger.Warn("could not read incoming message", zap.Error(err))
			nm = NetworkMessage{incoming, err, conn}
		} else {
			nm = NetworkMessage{incoming, nil, conn}
		}
		// handler is processing the request and updates msg
		ws.handler(&nm)

		err = tasks.Retry(func() error {
			return conn.WriteJSON(&nm.Msg)
		}, 3)
		if err != nil {
			logger.Error("could not send message", zap.Error(err))
			break
		}
	}
}

// handleStream registers the connection for broadcasting of stream messages
func (ws *wsServer) handleStream(wsc *websocket.Conn) {
	cid := ConnectionID(wsc)
	logger := ws.logger.
		With(zap.String("cid", cid))
	defer logger.Debug("stream handler done")

	ctx, cancel := context.WithCancel(ws.ctx)
	c := newConn(ctx, logger, wsc, cid, sendTimeout, ws.withPing)
	defer cancel()

	if !ws.broadcaster.Register(c) {
		logger.Warn("known connection")
		return
	}
	defer ws.broadcaster.Deregister(c)

	go c.ReadLoop()

	c.WriteLoop()
}
