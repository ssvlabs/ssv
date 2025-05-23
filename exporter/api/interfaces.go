package api

import (
	"fmt"
	"net"
	"time"
)

// Connection is an interface to abstract the actual websocket connection implementation
type Connection interface {
	Close() error
	RemoteAddr() net.Addr
}

// NetworkMessage wraps an actual message with more information
type NetworkMessage struct {
	Msg  Message
	Err  error
	Conn Connection
}

// QueryMessageHandler handles the given message
type QueryMessageHandler func(nm *NetworkMessage)

// ConnectionID calculates the id of the given Connection
func ConnectionID(conn Connection) string {
	if conn == nil {
		return ""
	}
	return fmt.Sprintf("conn-%s-%d",
		conn.RemoteAddr().String(), time.Now().UnixNano())
}
