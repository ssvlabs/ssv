package api

import "github.com/bloxapp/ssv/pubsub"

// NetworkMessage wraps an actual message with more information
type NetworkMessage struct {
	Msg  Message
	Err  error
	//Conn *websocket.Conn
}

// WebSocketServer is the interface exposed by this package
type WebSocketServer interface {
	Start(addr string) error
	IncomingSubject() pubsub.Subscriber
	OutboundSubject() pubsub.Publisher
}
