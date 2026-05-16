package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
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

	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Conn is a wrapper interface for websocket connections
type Conn interface {
	ID() string
	ReadNext() []byte
	Send(msg []byte)
	WriteLoop(logger *zap.Logger)
	ReadLoop(logger *zap.Logger)
	Close() error
	RemoteAddr() net.Addr
}

type conn struct {
	ctx       context.Context
	cancelCtx context.CancelFunc
	id        string
	ws        *websocket.Conn

	writeTimeout time.Duration

	read chan []byte
	send chan []byte

	writeLock sync.Locker

	withPing bool
}

func newConn(parent context.Context, ws *websocket.Conn, id string, writeTimeout time.Duration, withPing bool) Conn {
	ctx, cancel := context.WithCancel(parent)
	return &conn{
		ctx:          ctx,
		cancelCtx:    cancel,
		id:           id,
		ws:           ws,
		writeTimeout: writeTimeout,
		read:         make(chan []byte, chanSize),
		send:         make(chan []byte, chanSize),
		writeLock:    &sync.Mutex{},
		withPing:     withPing,
	}
}

// ID returns the connection id
func (c *conn) ID() string {
	return c.id
}

// RemoteAddr returns the remote address of the socket
func (c *conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

// Close cancels the conn ctx (signaling WriteLoop and ReadLoop to exit)
// and closes the underlying websocket. Idempotent: subsequent calls
// hit ws.Close on an already-closed connection, which returns an error
// that all callers ignore.
func (c *conn) Close() error {
	c.cancelCtx()
	if c.ws == nil {
		return nil
	}
	return c.ws.Close()
}

// ReadNext reads the next message
func (c *conn) ReadNext() []byte {
	return <-c.read
}

// Send queues msg for the WriteLoop. Non-blocking: if the queue is full the
// client cannot keep up, so we tear the conn down via Close — a silent
// gap in this stream is undetectable client-side, so reconnecting (with
// a fresh, consistent view) is the kindest outcome.
func (c *conn) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
		_ = c.Close()
	}
}

// WriteLoop a loop to activate writes on the socket. Always tears down
// on exit via c.Close so a write error reaches ReadLoop (via ctx +
// ws.Close) without waiting for handleStream's defer.
func (c *conn) WriteLoop(logger *zap.Logger) {
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	if c.withPing {
		t := time.NewTimer(pingInterval)
		defer t.Stop()
		go func() {
			defer cancel()
			c.pingLoop(ctx, logger)
		}()
	}

	for {
		select {
		case <-ctx.Done():
			c.writeLock.Lock()
			logger.Debug("context done, sending close message")
			// Best-effort graceful close-message; ws may already be
			// closed (c.Close cancels ctx and closes ws together), so
			// failure here is expected and not actionable.
			err := c.ws.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(c.writeTimeout))
			c.writeLock.Unlock()
			if err != nil {
				logger.Debug("could not send close message", zap.Error(err))
			}
			return
		case message := <-c.send:
			c.writeLock.Lock()
			_, err := c.sendMsg(message)
			c.writeLock.Unlock()
			if err != nil {
				logger.Warn("failed to send message", zap.Error(err))
				return
			}
		}
	}
}

// ReadLoop is a loop to read messages from the socket. Tearing down via
// c.Close on exit is essential: it cancels ctx, which is the only thing
// that unblocks WriteLoop's select on a quiet conn (no in-flight writes
// to fail). Without this, a client disconnect on an idle conn would
// leak the WriteLoop goroutine.
func (c *conn) ReadLoop(logger *zap.Logger) {
	defer func() { _ = c.Close() }()
	c.ws.SetReadLimit(maxMessageSize)
	// ping helps to keep the connection alive from our POV
	if c.withPing {
		// set deadline so ping messages won't exceed timeout
		_ = c.ws.SetReadDeadline(time.Now().Add(pingTimeout))
		// extend the deadline on every pong message
		c.ws.SetPongHandler(func(string) error {
			return c.ws.SetReadDeadline(time.Now().Add(pingTimeout))
		})
	}
	for {
		if c.ctx.Err() != nil {
			logger.Error("read loop stopped by context")
			break
		}
		mt, msg, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				logger.Error("unexpected close error", zap.Error(err))
			} else if isCloseError(err) {
				logger.Warn("connection closed error", zap.Error(err))
			} else {
				logger.Error("could not read message", zap.Error(err))
			}
			break
		}
		if mt == websocket.TextMessage {
			msg = bytes.TrimSpace(bytes.ReplaceAll(msg, newline, space))
			c.read <- msg
		}
	}
}

// pingLoop sends ping messages according to configured interval
func (c *conn) pingLoop(ctx context.Context, logger *zap.Logger) {
	t := time.NewTimer(pingInterval)
	for {
		if ctx.Err() != nil {
			return
		}
		t.Reset(pingInterval)
		<-t.C
		c.writeLock.Lock()
		logger.Debug("sending ping message")
		err := c.ws.WriteControl(websocket.PingMessage, pingMsg(c.ID()), time.Now().Add(c.writeTimeout))
		c.writeLock.Unlock()
		if err != nil {
			logger.Error("could not send ping message", zap.Error(err))
			return
		}
	}
}

// sendMsg sends the given message and returns the number of bytes that were written, plus the error.
// w.Close is always called so the websocket frame is flushed even when Write fails;
// the Write error wins over the Close error when both surface.
func (c *conn) sendMsg(msg []byte) (int, error) {
	_ = c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	w, err := c.ws.NextWriter(websocket.TextMessage)
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
