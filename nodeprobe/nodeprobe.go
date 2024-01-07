package nodeprobe

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	probeInterval = 24 * time.Second
)

type Node interface {
	Healthy(ctx context.Context) error
}

type Prober struct {
	logger           *zap.Logger
	interval         time.Duration
	nodes            map[string]Node
	nodesMu          sync.Mutex
	healthy          atomic.Bool
	cond             *sync.Cond
	unhealthyHandler func()
}

func NewProber(logger *zap.Logger, unhealthyHandler func(), nodes map[string]Node) *Prober {
	return &Prober{
		logger:           logger,
		unhealthyHandler: unhealthyHandler,
		interval:         probeInterval,
		nodes:            nodes,
		cond:             sync.NewCond(&sync.Mutex{}),
	}
}

func (p *Prober) Healthy(context.Context) (bool, error) {
	return p.healthy.Load(), nil
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

	var healthy atomic.Bool
	healthy.Store(true)
	var wg sync.WaitGroup
	p.nodesMu.Lock()
	for name, node := range p.nodes {
		wg.Add(1)
		go func(name string, node Node) {
			defer wg.Done()

			var err error
			defer func() {
				// Catch panics.
				if e := recover(); e != nil {
					err = fmt.Errorf("panic: %v", e)
				}
				if err != nil {
					// Update readiness and quit early.
					healthy.Store(false)
					cancel()
				}
			}()

			err = node.Healthy(ctx)
			if err != nil {
				p.logger.Error("node is not healthy", zap.String("node", name), zap.Error(err))
			}
		}(name, node)
	}
	p.nodesMu.Unlock()
	wg.Wait()

	// Update readiness.
	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	p.healthy.Store(healthy.Load())

	if !p.healthy.Load() {
		p.logger.Error("not all nodes are healthy")
		if h := p.unhealthyHandler; h != nil {
			h()
		}
		return
	}
	// Wake up any waiters.
	p.cond.Broadcast()
}

func (p *Prober) Wait() {
	p.logger.Info("waiting until nodes are healthy")

	p.cond.L.Lock()
	defer p.cond.L.Unlock()

	for !p.healthy.Load() {
		p.cond.Wait()
	}
}

func (p *Prober) AddNode(name string, node Node) {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	p.nodes[name] = node
}

// TODO: string constants are error-prone.
// Add a method to clients that returns their name or solve this in another way
func (p *Prober) CheckBeaconNodeHealth(ctx context.Context) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, p.interval)
	defer cancel()
	return p.nodes["consensus client"].Healthy(ctx)
}

func (p *Prober) CheckExecutionNodeHealth(ctx context.Context) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, p.interval)
	defer cancel()
	return p.nodes["execution client"].Healthy(ctx)
}

func (p *Prober) CheckEventSyncerHealth(ctx context.Context) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, p.interval)
	defer cancel()
	return p.nodes["event syncer"].Healthy(ctx)
}
