package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"net"
	"net/http"
	"time"
)

var (
	// pongWait time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// pingPeriod period to send ping messages. Must be less than pongWait.
	pingPeriod = (pongWait * 8) / 10

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
	WriteLoop()
	ReadLoop()
	Close() error
	RemoteAddr() net.Addr
}

type conn struct {
	logger *zap.Logger
	ctx    context.Context
	id     string
	ws     *websocket.Conn

	writeTimeout time.Duration

	read chan []byte
	send chan []byte
}

func newConn(ctx context.Context, logger *zap.Logger, ws *websocket.Conn, id string, writeTimeout time.Duration) Conn {
	return &conn{
		ctx:          ctx,
		logger:       logger.With(zap.String("who", "WSConn")),
		id:           id,
		ws:           ws,
		writeTimeout: writeTimeout,
		read:         make(chan []byte, chanSize),
		send:         make(chan []byte, chanSize),
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

// Close closes the connection
func (c *conn) Close() error {
	return c.ws.Close()
}

// ReadNext reads the next message
func (c *conn) ReadNext() []byte {
	return <-c.read
}

// Send sends the given message
func (c *conn) Send(msg []byte) {
	if len(c.send) >= chanSize {
		// don't send on full channel
		return
	}
	c.send <- msg
}

// WriteLoop a loop to activate writes on the socket
func (c *conn) WriteLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()
	for {
		select {
		case message := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(pongWait))
			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				c.logger.Error("could not read ws message", zap.Error(err))
				return
			}
			if _, err = w.Write(message); err != nil {
				c.logger.Error("could not write ws message", zap.Error(err))
				reportStreamOutbound(c.ws.RemoteAddr().String(), err)
				return
			}
			err = w.Close()
			reportStreamOutbound(c.ws.RemoteAddr().String(), err)
			if err != nil {
				c.logger.Error("could not close writer", zap.Error(err))
				return
			}
			var msg Message
			if err := json.Unmarshal(message, &msg); err != nil {
				c.logger.Error("could not parse msg", zap.Any("filter", msg.Filter), zap.Error(err))
			}
			c.logger.Debug("ws msg was sent", zap.Any("filter", msg.Filter))
		case <-ticker.C:
			c.logger.Debug("sending ping message")
			if err := c.ws.WriteControl(websocket.PingMessage, []byte{0, 0, 0, 0}, time.Now().Add(c.writeTimeout)); err != nil {
				c.logger.Error("could not send ping message", zap.Error(err))
				return
			}
		case <-c.ctx.Done():
			c.logger.Debug("context done, sending close message")
			if err := c.ws.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(c.writeTimeout)); err != nil {
				c.logger.Error("could not send close message", zap.Error(err))
				return
			}
		}
	}
}

// ReadLoop is a loop to read messages from the socket
func (c *conn) ReadLoop() {
	defer func() {
		_ = c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	err := c.ws.SetReadDeadline(time.Now().Add(pongWait))
	if err != nil {
		c.logger.Error("read loop stopped by set read deadline", zap.Error(err))
		return
	}
	c.ws.SetPongHandler(func(message string) error {
		// extend read limit on every pong message
		// this will keep the connection alive from our POV
		c.logger.Debug("pong received", zap.String("message", message))
		err := c.ws.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			c.logger.Error("pong handler - readDeadline", zap.Error(err))
		}
		return err
	})
	c.ws.SetPingHandler(func(message string) error {
		c.logger.Debug("ping received")
		err := c.ws.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(c.writeTimeout))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		}
		return err
	})
	for {
		if c.ctx.Err() != nil {
			c.logger.Error("read loop stopped by context")
			break
		}
		mt, msg, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				c.logger.Error("unexpected close error", zap.Error(err))
			} else if isCloseError(err) {
				c.logger.Warn("connection closed error", zap.Error(err))
			} else {
				c.logger.Error("could not read message", zap.Error(err))
			}
			break
		}
		if mt == websocket.TextMessage {
			msg = bytes.TrimSpace(bytes.Replace(msg, newline, space, -1))
			c.read <- msg
		}
	}
}

func isCloseError(err error) bool {
	_, ok := err.(*websocket.CloseError)
	return ok
}
