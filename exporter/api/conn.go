package api

import (
	"bytes"
	"context"
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
	pingPeriod = (pongWait * 9) / 10

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

// Conn is an interface representing connection
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
		logger:       logger,
		id:           id,
		ws:           ws,
		writeTimeout: writeTimeout,
		read:         make(chan []byte, chanSize),
		send:         make(chan []byte, chanSize),
	}
}

func (c *conn) ID() string {
	return c.id
}

//func (c *conn) Active() bool {
//	return c.active
//}

func (c *conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *conn) Close() error {
	return c.ws.Close()
}

func (c *conn) ReadNext() []byte {
	return <-c.read
}

func (c *conn) Send(msg []byte) {
	//if len(c.send) < chanSize {
	c.send <- msg
	//}
}

func (c *conn) WriteLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()
	for {
		select {
		case message /*, ok*/ := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(pongWait))
			/*if !ok {
				c.logger.Debug("send channel was closed, sending close message")
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}*/
			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				c.logger.Error("could not read ws message", zap.Error(err))
				return
			}
			if _, err = w.Write(message); err != nil {
				c.logger.Error("could not write ws message", zap.Error(err))
				reportStreamOutbound(c.ID(), err)
				return
			}
			// write queued messages
			//n := len(c.send)
			//for i := 0; i < n; i++ {
			//	w.Write(newline)
			//	w.Write(<-c.send)
			//}
			err = w.Close()
			reportStreamOutbound(c.ID(), err)
			if err != nil {
				c.logger.Error("could not close writer", zap.Error(err))
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				c.logger.Error("could not send ping message", zap.Error(err))
				return
			}
		case <-c.ctx.Done():
			c.logger.Debug("context done, sending close message")
			_ = c.write(websocket.CloseMessage, []byte{})
			return
		}
	}
}

func (c *conn) ReadLoop() {
	defer func() {
		_ = c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { _ = c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
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

// write writes a message with the given message type and payload.
func (c *conn) write(mt int, payload []byte) error {
	if err := c.ws.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return err
	}
	return c.ws.WriteMessage(mt, payload)
}

func isCloseError(err error) bool {
	_, ok := err.(*websocket.CloseError)
	return ok
}
