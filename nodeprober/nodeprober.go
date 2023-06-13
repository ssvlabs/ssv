package nodeprober

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	probeInterval = 1 * time.Minute
)

type StatusChecker interface {
	IsReady(ctx context.Context) (bool, error)
}

type Prober struct {
	logger zap.Logger
	nodes  []StatusChecker
	ready  atomic.Bool
	cond   sync.Cond
}

func NewProber(logger *zap.Logger, nodes ...StatusChecker) *Prober {
	return &Prober{nodes: nodes}
}

func (p *Prober) IsReady(context.Context) (bool, error) {
	return p.ready.Load(), nil
}

func (p *Prober) Run(ctx context.Context) error {
	ticker := time.NewTicker(probeInterval)
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
	ctx, cancel := context.WithTimeout(ctx, probeInterval)
	defer cancel()

	var ready atomic.Bool
	ready.Store(true)
	var wg sync.WaitGroup
	for _, node := range p.nodes {
		wg.Add(1)
		go func(node StatusChecker) {
			defer wg.Done()

			var syncing bool
			var err error
			defer func() {
				// Catch panics.
				if e := recover(); e != nil {
					err = fmt.Errorf("panic: %v", e)
				}
				if err != nil || syncing {
					// Update readiness and quit early.
					ready.Store(false)
					cancel()
				}
			}()
			syncing, err = node.IsReady(ctx)
			if err != nil {
				p.logger.Error("node is not ready", zap.Error(err))
			} else if syncing {
				p.logger.Error("node is syncing")
			}
		}(node)
	}
	wg.Wait()

	// Update readiness.
	p.cond.L.Lock()
	defer p.cond.L.Unlock()
	p.ready.Store(ready.Load())

	// Wake up any waiters.
	if p.ready.Load() {
		p.cond.Broadcast()
	}
}

func (p *Prober) Wait() {
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	for !p.ready.Load() {
		p.cond.Wait()
	}
}
