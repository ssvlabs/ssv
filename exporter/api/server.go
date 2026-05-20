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
	// listener is ready to accept connections, plus a channel that emits a
	// non-graceful serve error (the channel is closed silently when the
	// ctx-driven shutdown completes).
	Start(addr string) (string, <-chan error, error)
	BroadcastFeed() *event.Feed
	UseQueryHandler(handler QueryMessageHandler)
}

// wsServer is an implementation of WebSocketServer
type wsServer struct {
	logger *zap.Logger
	ctx    context.Context

	router    *http.ServeMux
	endpoints []string
	handler   QueryMessageHandler

	broadcaster Broadcaster
	// outFeed is a subject for writing messages
	outFeed *event.Feed

	withPing bool
}

// NewWsServer creates a new instance.
func NewWsServer(ctx context.Context, logger *zap.Logger, handler QueryMessageHandler, mux *http.ServeMux, withPing bool) WebSocketServer {
	ws := wsServer{
		ctx:         ctx,
		logger:      logger.Named(log.NameWSServer),
		handler:     handler,
		router:      mux,
		broadcaster: newBroadcaster(logger),
		outFeed:     new(event.Feed),
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
// callers using a :0 port can discover the kernel-assigned one) and a channel
// that emits a non-graceful serve error (the channel is closed silently when
// the ctx-driven shutdown completes). The server shuts down when the ctx
// passed to NewWsServer is canceled.
func (ws *wsServer) Start(addr string) (string, <-chan error, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	boundAddr := l.Addr().String()

	const timeout = 3 * time.Second
	httpServer := &http.Server{
		Handler:      ws.router,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}

	ws.logger.Info("starting", fields.Address(boundAddr), zap.Strings("endPoints", ws.endpoints))

	ws.broadcaster.FromFeed(ws.ctx, ws.outFeed)

	go func() {
		<-ws.ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			ws.logger.Error("shutdown failed", zap.Error(err))
		}
	}()

	serveErr := make(chan error, 1)
	go func() {
		defer close(serveErr)
		if err := httpServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	return boundAddr, serveErr, nil
}

// BroadcastFeed returns the feed for stream messages
func (ws *wsServer) BroadcastFeed() *event.Feed {
	return ws.outFeed
}

// RegisterHandler mounts a websocket endpoint at endPoint, served by handler.
func (ws *wsServer) RegisterHandler(name, endPoint string, handler func(*websocket.Conn)) {
	wrappedHandler := func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, w.Header())
		if err != nil {
			ws.logger.Error("could not upgrade connection", zap.Error(err))
			return
		}
		ws.logger.Debug("new websocket connection")
		defer conn.Close()

		// http.Server.Shutdown doesn't close upgraded websockets, so on
		// server ctx cancel we close ours up-front — unblocking any
		// read/write pending in the handler so the deferred close above
		// gets to run.
		stop := context.AfterFunc(ws.ctx, func() { _ = conn.Close() })
		defer stop()

		handler(conn)
	}

	otelHandler := otelhttp.NewHandler(http.HandlerFunc(wrappedHandler), name)
	ws.router.Handle(endPoint, otelHandler)
	ws.endpoints = append(ws.endpoints, endPoint)
}

// handleQuery receives query message and respond async. The websocket
// is closed by wsServer.RegisterHandler's wrapper on return.
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
			if err := ws.ctx.Err(); err != nil {
				return err
			}
			return conn.WriteJSON(&networkMessage.Msg)
		}, 3)
		if err != nil {
			logger.Error("could not send message", zap.Error(err))
			break
		}
	}
}

// handleStream registers the connection for broadcasting of stream
// messages. The websocket is closed by wsServer.RegisterHandler's
// wrapper on return; handleStream only releases what it owns: the
// *conn wrapper's ctx and the broadcaster registration.
func (ws *wsServer) handleStream(wsc *websocket.Conn) {
	cid := ConnectionID(wsc)
	logger := ws.logger.
		With(fields.ConnectionID(cid)).
		With(zap.String("remote addr", wsc.RemoteAddr().String()))
	defer logger.Debug("stream handler done")

	c := newConn(ws.ctx, wsc, cid, sendTimeout, ws.withPing)
	defer c.Cancel()

	if !ws.broadcaster.Register(c) {
		logger.Warn("known connection")
		return
	}
	defer ws.broadcaster.Deregister(c)

	go c.ReadLoop(logger)

	c.WriteLoop(logger)
}
