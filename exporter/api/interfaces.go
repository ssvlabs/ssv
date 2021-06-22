package api

import (
	"github.com/bloxapp/ssv/pubsub"
	"io"
)

// Connection is an interface to abstract the actual websocket connection implementation
type Connection interface {
	io.ReadWriteCloser
}

// NetworkMessage wraps an actual message with more information
type NetworkMessage struct {
	Msg  Message
	Err  error
	Conn Connection
}

// WebSocketServer is the interface exposed by this package
type WebSocketServer interface {
	Start(addr string) error
	IncomingSubject() pubsub.Subscriber
	OutboundSubject() pubsub.Publisher
}

// EndPointHandler is an interface to abstract the actual websocket handler implementation
type EndPointHandler = func(conn Connection)

// WebSocketAdapter is an abstraction to decouple actual library implementation
type WebSocketAdapter interface {
	RegisterHandler(endPoint string, handler EndPointHandler)
	Send(conn Connection, v interface{}) error
	Receive(conn Connection, v interface{}) error
	ConnectionID(conn Connection) string
}
