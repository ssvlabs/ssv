package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/utils/tasks"
)

const (
	sendTimeout = 3 * time.Second
)

// WebSocketServer is responsible for managing all
type WebSocketServer interface {
	// Start binds a listener on addr and begins serving in the background.
	// It returns the bound address (useful when addr has a :0 port) once the
	// listener is ready to accept connections. Serve-loop errors are logged,
	// not returned.
	Start(addr string) (string, error)
	BroadcastFeed() *event.Feed
	UseQueryHandler(handler QueryMessageHandler)
}

// wsServer is an implementation of WebSocketServer
type wsServer struct {
	logger *zap.Logger
	ctx    context.Context

	handler QueryMessageHandler

	broadcaster Broadcaster

	router     *http.ServeMux
	// out is a subject for writing messages
	out      *event.Feed
	withPing bool
}

// NewWsServer creates a new instance.
func NewWsServer(ctx context.Context, logger *zap.Logger, handler QueryMessageHandler, mux *http.ServeMux, withPing bool) *wsServer {
	ws := wsServer{
		ctx:         ctx,
		logger:      logger.Named(log.NameWSServer),
		handler:     handler,
		router:      mux,
		broadcaster: newBroadcaster(logger),
		out:         new(event.Feed),
		withPing:    withPing,
	}
	ws.RegisterHandler("query", "/query", ws.handleQuery)
	ws.RegisterHandler("stream", "/stream", ws.handleStream)
	return &ws
}

func (ws *wsServer) UseQueryHandler(handler QueryMessageHandler) {
	ws.handler = handler
}

// Start binds a listener on addr and serves the websocket server in the
// background. It returns the bound address once the listener is ready (so
// callers using a :0 port can discover the kernel-assigned one). The server
// shuts down when the ctx passed to NewWsServer is canceled
func (ws *wsServer) Start(addr string) (string, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w", addr, err)
	}
	boundAddr := l.Addr().String()

	const timeout = 3 * time.Second
	httpServer := &http.Server{
		Handler:      ws.router,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	ws.logger.Info("starting", fields.Address(boundAddr), zap.Strings("endPoints", []string{"/query", "/stream"}))

	ws.broadcaster.FromFeed(ws.ctx, ws.out)

	go func() {
		<-ws.ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			ws.logger.Error("shutdown failed", zap.Error(err))
		}
	}()

	go func() {
		if err := httpServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			ws.logger.Error("serve loop exited", zap.Error(err))
		}
	}()

	return boundAddr, nil
}

// BroadcastFeed returns the feed for stream messages
func (ws *wsServer) BroadcastFeed() *event.Feed {
	return ws.out
}

// RegisterHandler registers an end point
func (ws *wsServer) RegisterHandler(name, endPoint string, handler func(conn *websocket.Conn)) {
	wrappedHandler := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, w.Header())
		if err != nil {
			ws.logger.Error("could not upgrade connection", zap.Error(err))
			return
		}
		ws.logger.Debug("new websocket connection")
		defer func() {
			ws.logger.Debug("closing connection")
			err := conn.Close()
			if err != nil {
				ws.logger.Error("could not close connection", zap.Error(err))
			}
		}()
		handler(conn)
	}

	otelHandler := otelhttp.NewHandler(http.HandlerFunc(wrappedHandler), name)
	ws.router.Handle(endPoint, otelHandler)
}

// handleQuery receives query message and respond async
func (ws *wsServer) handleQuery(conn *websocket.Conn) {
	if ws.handler == nil {
		return
	}
	cid := ConnectionID(conn)
	logger := ws.logger.
		With(fields.ConnectionID(cid)).
		With(zap.String("remote addr", conn.RemoteAddr().String()))
	logger.Debug("handles query requests")

	for {
		if ws.ctx.Err() != nil {
			logger.Debug("context was done")
		}
		var incoming Message
		var networkMessage NetworkMessage
		err := conn.ReadJSON(&incoming)
		if err != nil {
			if isCloseError(err) {
				logger.Debug("failed to read message; connection was closed", zap.Error(err))
				return
			}
			logger.Warn("could not read incoming message", zap.Error(err))
			networkMessage = NetworkMessage{incoming, err, conn}
		} else {
			networkMessage = NetworkMessage{incoming, nil, conn}
		}
		// handler is processing the request and updates msg
		ws.handler(&networkMessage)

		err = tasks.Retry(func() error {
			return conn.WriteJSON(&networkMessage.Msg)
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
		With(fields.ConnectionID(cid)).
		With(zap.String("remote addr", wsc.RemoteAddr().String()))
	defer logger.Debug("stream handler done")

	ctx, cancel := context.WithCancel(ws.ctx)
	c := newConn(ctx, wsc, cid, sendTimeout, ws.withPing)
	defer cancel()

	if !ws.broadcaster.Register(c) {
		logger.Warn("known connection")
		return
	}
	defer ws.broadcaster.Deregister(c)

	go c.ReadLoop(logger)

	c.WriteLoop(logger)
}
