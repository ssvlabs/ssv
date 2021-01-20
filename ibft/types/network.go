package types

type Networker interface {
	Broadcast(msg *Message) error
}
