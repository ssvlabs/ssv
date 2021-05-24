package pubsub

import "go.uber.org/zap"

// Observer interface that notified by the subject registered to
type Observer interface {
	InformObserver(interface{}) // TODO need to use golang channels
	GetObserverID() string
}

// BaseObserver struct that implements Observer
type BaseObserver struct {
	ID     string
	Logger zap.Logger
}

// InformObserver get inform by the subject and passes interface that need to cast for specific type
func (b BaseObserver) InformObserver(i interface{}) {
	b.Logger.Info("InformObserver")
}

// GetObserverID get observer ID
func (b BaseObserver) GetObserverID() string {
	return "BaseObserver"
}

