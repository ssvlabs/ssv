package types

type Networker interface {
	// ReceivedMsgChan is a channel that forwards new propagated messages to a subscriber
	// message signature verification should be done in the specific networker implementation
	ReceivedMsgChan() chan *Message

	// Broadcast propagates a signed message to all peers
	Broadcast(msg *Message) error
}
