package nodeprobe

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

const (
	probeInterval = 1 * time.Minute
)

type StatusChecker interface {
	IsReady(ctx context.Context) (bool, error)
}

type Prober struct {
	logger         *zap.Logger
	interval       time.Duration
	nodes          []StatusChecker
	ready          atomic.Bool
	cond           *sync.Cond
	unreadyHandler atomic.Pointer[func()]
}

func NewProber(logger *zap.Logger, nodes ...StatusChecker) *Prober {
	return &Prober{
		logger:   logger,
		interval: probeInterval,
		nodes:    nodes,
		cond:     sync.NewCond(&sync.Mutex{}),
	}
}

func (p *Prober) IsReady(context.Context) (bool, error) {
	return p.ready.Load(), nil
}

func (p *Prober) Start(ctx context.Context) {
	go func() {
		if err := p.Run(ctx); err != nil {
			p.logger.Error("finished probing nodes", zap.Error(err))
		}
	}()
}

func (p *Prober) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		p.probe(ctx)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			continue
		}
	}
}

func (p *Prober) probe(ctx context.Context) {
	// Query all nodes in parallel.
	ctx, cancel := context.WithTimeout(ctx, p.interval)
	defer cancel()

	var allNodesReady atomic.Bool
	allNodesReady.Store(true)
	var wg sync.WaitGroup
	for _, node := range p.nodes {
		wg.Add(1)
		go func(node StatusChecker) {
			defer wg.Done()

			var ready bool
			var err error
			defer func() {
				// Catch panics.
				if e := recover(); e != nil {
					err = fmt.Errorf("panic: %v", e)
				}
				if err != nil || !ready {
					// Update readiness and quit early.
					allNodesReady.Store(false)
					cancel()
				}
			}()

			logger := p.logger.With(fields.Type(node))

			ready, err = node.IsReady(ctx)
			if err != nil {
				logger.Error("failed to check if node is ready", zap.Error(err))
			} else if !ready {
				logger.Error("node is not ready")
			} else {
				logger.Info("node is ready")
			}
		}(node)
	}
	wg.Wait()

	// Update readiness.
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	p.ready.Store(allNodesReady.Load())

	if !p.ready.Load() {
		p.logger.Warn("not all nodes are ready")
		if h := p.unreadyHandler.Load(); h != nil {
			(*h)()
		}
		return
	}
	// Wake up any waiters.
	p.cond.Broadcast()
}

func (p *Prober) Wait() {
	p.logger.Info("waiting until nodes are ready")

	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	for !p.ready.Load() {
		p.cond.Wait()
	}
}

func (p *Prober) SetUnreadyHandler(h func()) {
	p.unreadyHandler.Store(&h)
}
