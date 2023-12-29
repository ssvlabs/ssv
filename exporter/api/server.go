package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/utils/tasks"
)

const (
	sendTimeout = 3 * time.Second
)

// WebSocketServer is responsible for managing all
type WebSocketServer interface {
	Start(logger *zap.Logger, addr string) error
	BroadcastFeed() *event.Feed
	UseQueryHandler(handler QueryMessageHandler)
}

// wsServer is an implementation of WebSocketServer
type wsServer struct {
	ctx     context.Context
	metrics metrics

	handler QueryMessageHandler

	broadcaster Broadcaster

	router *http.ServeMux
	// out is a subject for writing messages
	out      *event.Feed
	withPing bool
}

// NewWsServer creates a new instance
func NewWsServer(ctx context.Context, metrics metrics, handler QueryMessageHandler, mux *http.ServeMux, withPing bool) WebSocketServer {
	ws := wsServer{
		ctx:         ctx,
		metrics:     metrics,
		handler:     handler,
		router:      mux,
		broadcaster: newBroadcaster(),
		out:         new(event.Feed),
		withPing:    withPing,
	}
	return &ws
}

func (ws *wsServer) UseQueryHandler(handler QueryMessageHandler) {
	ws.handler = handler
}

// Start starts the websocket server and the broadcaster
func (ws *wsServer) Start(logger *zap.Logger, addr string) error {
	logger = logger.Named(logging.NameWSServer)

	ws.RegisterHandler(logger, "/query", ws.handleQuery)
	ws.RegisterHandler(logger, "/stream", ws.handleStream)

	go func() {
		if err := ws.broadcaster.FromFeed(logger, ws.out); err != nil {
			logger.Debug("failed to pull messages from feed")
		}
	}()

	logger.Info("starting", fields.Address(addr), zap.Strings("endPoints", []string{"/query", "/stream"}))

	const timeout = 3 * time.Second

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      ws.router,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	err := httpServer.ListenAndServe()
	if err != nil {
		logger.Warn("could not start", zap.Error(err))
	}
	return err
}

// BroadcastFeed returns the feed for stream messages
func (ws *wsServer) BroadcastFeed() *event.Feed {
	return ws.out
}

// RegisterHandler registers an end point
func (ws *wsServer) RegisterHandler(logger *zap.Logger, endPoint string, handler func(logger *zap.Logger, conn *websocket.Conn)) {
	ws.router.HandleFunc(endPoint, func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, w.Header())
		logger := logger.With(zap.String("remote addr", conn.RemoteAddr().String()))
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
		handler(logger, conn)
	})
}

// handleQuery receives query message and respond async
func (ws *wsServer) handleQuery(logger *zap.Logger, conn *websocket.Conn) {
	if ws.handler == nil {
		return
	}
	cid := ConnectionID(conn)
	logger = logger.With(fields.ConnectionID(cid))
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
				logger.Debug("failed to read message; connection was closed", zap.Error(err))
				return
			}
			logger.Warn("could not read incoming message", zap.Error(err))
			nm = NetworkMessage{incoming, err, conn}
		} else {
			nm = NetworkMessage{incoming, nil, conn}
		}
		// handler is processing the request and updates msg
		ws.handler(logger, &nm)

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
func (ws *wsServer) handleStream(logger *zap.Logger, wsc *websocket.Conn) {
	cid := ConnectionID(wsc)
	logger = logger.With(fields.ConnectionID(cid))
	defer logger.Debug("stream handler done")

	ctx, cancel := context.WithCancel(ws.ctx)
	c := newConn(ctx, ws.metrics, wsc, cid, sendTimeout, ws.withPing)
	defer cancel()

	if !ws.broadcaster.Register(c) {
		logger.Warn("known connection")
		return
	}
	defer ws.broadcaster.Deregister(c)

	go c.ReadLoop(logger)

	c.WriteLoop(logger)
}
