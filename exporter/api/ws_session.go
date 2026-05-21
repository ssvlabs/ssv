package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	// pingTimeout time allowed to read the next pong message from the peer.
	pingTimeout = 60 * time.Second

	// pingInterval period to send ping messages. Must be less than pingTimeout.
	pingInterval = (pingTimeout * 8) / 10

	// maxMessageSize max msg size allowed from peer.
	maxMessageSize = int64(1024)

	chanSize = 256
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// wsSession is the server-side state for one /stream client: a buffered
// send queue drained by WriteLoop, a ReadLoop that drives the protocol
// from the read side, and a ctx that ties them together. Paired with
// wsServer in the same way one session pairs with the multiplexing
// server that accepted it. Does not own the underlying *websocket.Conn
// lifecycle — wsServer's wrappedHandler closes ws on return.
type wsSession struct {
	ctx       context.Context
	cancelCtx context.CancelFunc
	id        string
	ws        *websocket.Conn

	writeTimeout time.Duration

	send chan []byte

	writeLock sync.Locker

	withPing bool
}

func newWSSession(parent context.Context, ws *websocket.Conn, id string, writeTimeout time.Duration, withPing bool) *wsSession {
	ctx, cancel := context.WithCancel(parent)
	return &wsSession{
		ctx:          ctx,
		cancelCtx:    cancel,
		id:           id,
		ws:           ws,
		writeTimeout: writeTimeout,
		send:         make(chan []byte, chanSize),
		writeLock:    &sync.Mutex{},
		withPing:     withPing,
	}
}

// ID returns the session id (derived from the underlying ws remote addr).
func (s *wsSession) ID() string {
	return s.id
}

// Close releases this session's own resources: it cancels its ctx, which
// signals WriteLoop and ReadLoop to wind down. The underlying websocket
// is intentionally NOT touched — that lifecycle belongs to whoever
// passed ws into newWSSession (wsServer's wrappedHandler, which closes
// ws from its own defer). As a consequence, a ReadLoop parked in
// ReadMessage stays parked until ws is closed externally; it then exits
// cleanly on the next scheduling, which may briefly outlive the caller
// that triggered Close.
//
// Safe from any goroutine; idempotent.
func (s *wsSession) Close() {
	s.cancelCtx()
}

// Send queues msg for the WriteLoop. Non-blocking. Three outcomes:
//   - session already canceled: drop msg, no-op. We've decided to drop
//     this client; further queueing would just be drained-and-delivered
//     before WriteLoop noticed and exited (Go's select is randomized,
//     not priority-ordered).
//   - queue has room: enqueue.
//   - queue full: tear the session down via Close. A silent gap in
//     this stream is undetectable client-side, so reconnecting (with a
//     fresh, consistent view) is the kindest outcome.
//
// The ctx-check is TOCTOU-racy with concurrent cancel, but the window
// is narrow — at worst a handful of messages queue right after cancel,
// versus the unbounded draining the broadcaster would otherwise feed.
func (s *wsSession) Send(msg []byte) {
	if s.ctx.Err() != nil {
		return
	}
	select {
	case s.send <- msg:
	default:
		s.Close()
	}
}

// WriteLoop a loop to activate writes on the socket. Always tears down
// on exit via s.Close, propagating ctx-cancel so ReadLoop notices on
// the next scheduling (ws stays open until wsServer's wrappedHandler
// closes it, which then unblocks ReadLoop's ReadMessage).
//
// The pre-check on ctx at the top of each iteration is load-bearing:
// Go's select picks randomly among ready cases, so without it, queued
// messages would still drain to the wire after Send-overflow canceled
// the session — defeating the prompt-shutdown intent of dropping a
// slow client.
func (s *wsSession) WriteLoop(logger *zap.Logger) {
	defer s.Close()

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	if s.withPing {
		go func() {
			defer cancel()
			s.pingLoop(ctx, logger)
		}()
	}

drain:
	for {
		if ctx.Err() != nil {
			break drain
		}
		select {
		case <-ctx.Done():
			break drain
		case message := <-s.send:
			s.writeLock.Lock()
			_, err := s.sendMsg(message)
			s.writeLock.Unlock()
			if err != nil {
				// Write error: bail out without the graceful close
				// frame — the next write would just fail too.
				logger.Warn("failed to send message", zap.Error(err))
				return
			}
		}
	}

	s.writeLock.Lock()
	logger.Debug("context done, sending close message")
	// Best-effort graceful close-message; if ws is already closed
	// (e.g. wrappedHandler beat us to it on shutdown), failure here
	// is expected and not actionable.
	err := s.ws.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(s.writeTimeout))
	s.writeLock.Unlock()
	if err != nil {
		logger.Debug("could not send close message", zap.Error(err))
	}
}

// ReadLoop drives the read side of the websocket so the pong handler
// fires and a peer disconnect surfaces as a ReadMessage error. Inbound
// data frames are discarded — *wsSession is push-only; the broadcaster
// never reads from this side.
//
// Tearing down via s.Close on exit is essential: it cancels ctx, which
// is the only thing that unblocks WriteLoop's select on a quiet session
// (no in-flight writes to fail). Without this, a client disconnect on
// an idle session would leak the WriteLoop goroutine.
func (s *wsSession) ReadLoop(logger *zap.Logger) {
	defer s.Close()
	s.ws.SetReadLimit(maxMessageSize)
	// ping helps to keep the connection alive from our POV
	if s.withPing {
		// set deadline so ping messages won't exceed timeout
		_ = s.ws.SetReadDeadline(time.Now().Add(pingTimeout))
		// extend the deadline on every pong message
		s.ws.SetPongHandler(func(string) error {
			return s.ws.SetReadDeadline(time.Now().Add(pingTimeout))
		})
	}
	for {
		if _, _, err := s.ws.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				logger.Error("unexpected close error", zap.Error(err))
			} else if isCloseError(err) {
				logger.Warn("connection closed error", zap.Error(err))
			} else {
				logger.Error("could not read message", zap.Error(err))
			}
			break
		}
	}
}

// pingLoop sends ping messages according to configured interval. Exits
// promptly on ctx cancel (instead of blocking up to pingInterval on a
// quiet session).
func (s *wsSession) pingLoop(ctx context.Context, logger *zap.Logger) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.writeLock.Lock()
		logger.Debug("sending ping message")
		err := s.ws.WriteControl(websocket.PingMessage, pingMsg(s.ID()), time.Now().Add(s.writeTimeout))
		s.writeLock.Unlock()
		if err != nil {
			logger.Error("could not send ping message", zap.Error(err))
			return
		}
	}
}

// sendMsg sends the given message and returns the number of bytes that were written, plus the error.
// w.Close is always called so the websocket frame is flushed even when Write fails;
// the Write error wins over the Close error when both surface.
func (s *wsSession) sendMsg(msg []byte) (int, error) {
	_ = s.ws.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	w, err := s.ws.NextWriter(websocket.TextMessage)
	if err != nil {
		return 0, fmt.Errorf("could not create ws writer: %w", err)
	}
	n, werr := w.Write(msg)
	cerr := w.Close()
	if werr != nil {
		return n, fmt.Errorf("could not write ws message: %w", werr)
	}
	if cerr != nil {
		return n, fmt.Errorf("could not close writer: %w", cerr)
	}
	return n, nil
}

// isCloseError determines whether the given error is of CloseError type
func isCloseError(err error) bool {
	var closeError *websocket.CloseError
	return errors.As(err, &closeError)
}

// pingMsg construct a ping message
func pingMsg(cid string) []byte {
	return []byte{cid[0], cid[1], cid[3], cid[4]}
}
