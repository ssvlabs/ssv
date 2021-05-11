package pubsub

import "go.uber.org/zap"

// Observer interface
type Observer interface {
	InformObserver(interface{})
	GetObserverID() string
}

// BaseObserver struct
type BaseObserver struct {
	ID     string
	Logger zap.Logger
}
