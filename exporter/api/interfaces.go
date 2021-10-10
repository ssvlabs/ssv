package api

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// Connection is an interface to abstract the actual websocket connection implementation
type Connection interface {
	Close() error
	LocalAddr() net.Addr
}

// NetworkMessage wraps an actual message with more information
type NetworkMessage struct {
	Msg  Message
	Err  error
	Conn Connection
}

// QueryMessageHandler handles the given message
type QueryMessageHandler func(nm *NetworkMessage)

// EndPointHandler is an interface to abstract the actual websocket handler implementation
type EndPointHandler = func(conn Connection)

// WebSocketAdapter is an abstraction to decouple actual library implementation
type WebSocketAdapter interface {
	RegisterHandler(mux *http.ServeMux, endPoint string, handler EndPointHandler)
	Send(conn Connection, v interface{}) error
	Receive(conn Connection, v interface{}) error
	IsCloseError(err error) bool
}

// ConnectionID calculates the id of the given Connection
func ConnectionID(conn Connection) string {
	if conn == nil {
		return ""
	}
	return fmt.Sprintf("conn-%s-%d",
		conn.LocalAddr().String(), time.Now().UnixNano())
}
