package types

type Networker interface {
	ReceivedMsgChan() <-chan *Message
	Broadcast(msg *Message) error
}
