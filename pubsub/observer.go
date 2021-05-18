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

// InformObserver informs observer
func (b BaseObserver) InformObserver(i interface{}) {
	b.Logger.Info("InformObserver")
}

// GetObserverID get observer ID
func (b BaseObserver) GetObserverID() string {
	return "BaseObserver"
}

