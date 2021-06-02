package ibft

import (
	"context"
	"go.uber.org/zap"
	"sync"
	"time"
)

var dispatchInterval = 10 * time.Second

type Dispatcher interface {
	Queue(IbftReadOnly)
	Dispatch()
	StartInterval()
}

type dispatcher struct {
	ctx    context.Context
	logger *zap.Logger

	waiting []IbftReadOnly
	mut     sync.Mutex
}

func NewDispatcher(ctx context.Context, logger *zap.Logger) Dispatcher {
	d := dispatcher{
		ctx:     ctx,
		logger:  logger,
		waiting: []IbftReadOnly{},
		mut:     sync.Mutex{},
	}
	return &d
}

func (d *dispatcher) Queue(ibftInstance IbftReadOnly) {
	d.mut.Lock()
	defer d.mut.Unlock()

	d.waiting = append(d.waiting, ibftInstance)
	pubKey := ibftInstance.(*ibftReadOnly).validatorShare.PublicKey.SerializeToHexStr()
	d.logger.Debug("ibft sync was queued", zap.String("pubKey", pubKey))
}

func (d *dispatcher) Dispatch() {
	d.mut.Lock()
	if len(d.waiting) == 0 {
		return
	}
	ibftInstance := d.waiting[0]
	d.waiting = d.waiting[1:]
	d.mut.Unlock()

	go func() {
		pubKey := ibftInstance.(*ibftReadOnly).validatorShare.PublicKey.SerializeToHexStr()
		d.logger.Debug("ibft sync was dispatched", zap.String("pubKey", pubKey))
		ibftInstance.Sync()
	}()
}

func (d *dispatcher) StartInterval() {
	ticker := time.NewTicker(dispatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.Dispatch()
		case <-d.ctx.Done():
			d.logger.Debug("Context closed, exiting dispatcher interval routine")
			return
		}
	}
}
