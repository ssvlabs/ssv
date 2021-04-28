package pubsub

import "go.uber.org/zap"

type Observer interface {
	Update(interface{})
	getID() string
}

type BaseObserver struct {
	Id     string
	Logger zap.Logger
}

func (b *BaseObserver) update(msg interface{}) {
	b.Logger.Info("observer got updated", zap.Any("msg", msg))
}

func (b *BaseObserver) getID() string {
	return b.Id
}
