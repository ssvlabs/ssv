package pubsub

import "go.uber.org/zap"

// Observer interface
type Observer interface {
	Update(interface{})
	GetID() string
}

// BaseObserver struct
type BaseObserver struct {
	ID     string
	Logger zap.Logger
}


