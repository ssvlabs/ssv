package nodeprobe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

type nodeName string

const (
	cl          = "consensus client"
	el          = "execution client"
	eventSyncer = "event-syncer"
)

// Node represents a node being probed.
type Node interface {
	Healthy(ctx context.Context) error
}

const (
	probeFrequency = 60 * time.Second
)

// Prober probes(monitors) the nodes it is configured with. It can do it periodically in the background via
// Start func (which can crash the process via logger.Fatal call if it finds out that one of the nodes being
// probed is not healthy), or it can probe on demand via Probe func.
// It is used to make sure the Ethereum nodes (CL, EL) are up and running, and they are healthy enough to for
// SSV node to be able to perform duties.
type Prober struct {
	logger *zap.Logger

	// nodesMu protects access to nodes, needed to handle the case when "event syncer" is added dynamically later on
	// when the Prober is already running.
	nodesMu sync.Mutex
	nodes   map[nodeName]Node
}

func NewProber(logger *zap.Logger, consensusClient, executionClient Node) *Prober {
	return &Prober{
		logger: logger,
		nodes: map[nodeName]Node{
			el: executionClient,

			// Underlying options.Beacon's value implements nodeprobe.StatusChecker.
			// However, as it uses spec's specssv.BeaconNode interface, avoiding type assertion requires modifications in spec.
			// If options.Beacon doesn't implement nodeprobe.StatusChecker due to a mistake, this would panic early.
			cl: consensusClient,
		},
	}
}

func (p *Prober) Start(ctx context.Context) {
	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		func() {
			probeCtx, cancel := context.WithTimeout(ctx, probeFrequency)
			defer cancel()

			if err := p.Probe(probeCtx); err != nil {
				p.logger.Fatal("Ethereum node(s) are either out of sync or down. Ensure the nodes are healthy to resume.", zap.Error(err))
			}
		}()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			continue
		}
	}
}

func (p *Prober) Probe(ctx context.Context) error {
	// Probe all nodes in parallel, use cancel to quit early canceling irrelevant workers.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errsCh := make(chan error)

	p.nodesMu.Lock()
	nodes := p.nodes
	p.nodesMu.Unlock()

	for name, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := p.probeNode(ctx, name, node)
			if err != nil {
				// Relay the error and quit early.
				errsCh <- err
				cancel()
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errsCh)
	}()

	var errs error
	for err := range errsCh {
		errs = errors.Join(errs, err)
	}
	if errs != nil {
		errs = fmt.Errorf("probe health-check failed: %w", errs)
	}
	return errs
}

func (p *Prober) probeNode(ctx context.Context, name nodeName, node Node) (err error) {
	defer func() {
		// Catch panics to present these (however unlikely they are) as a readable error-message.
		if e := recover(); e != nil {
			err = fmt.Errorf("panic: %v", e)
		}
	}()

	// Retry health-check multiple times to make sure we do not classify an occasional glitch (or a network blip)
	// as node being unhealthy. Failing on the very 1st failed request would be too drastic a measure given it
	// may result into SSV node restart.
	const healthTimeout = 10 * time.Second
	const maxAttempt = int(probeFrequency / healthTimeout)
	for attempt := 1; attempt <= maxAttempt; attempt++ {
		err = func() error {
			healthCtx, cancel := context.WithTimeout(ctx, healthTimeout)
			defer cancel()

			return node.Healthy(healthCtx)
		}()
		if err == nil {
			return nil // success
		}
	}

	// All retries failed.

	if errors.Is(err, context.Canceled) {
		// The caller canceled probing, it's not an error then.
		return nil
	}

	return fmt.Errorf("%s is unhealthy: %w", name, err)
}

func (p *Prober) AddEventSyncer(node Node) {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	p.nodes[eventSyncer] = node
}

func (p *Prober) CheckBeaconNodeHealth(ctx context.Context) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	return p.nodes[cl].Healthy(ctx)
}

func (p *Prober) CheckExecutionNodeHealth(ctx context.Context) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	return p.nodes[el].Healthy(ctx)
}

func (p *Prober) CheckEventSyncerHealth(ctx context.Context) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	es, ok := p.nodes[eventSyncer]
	if !ok {
		return fmt.Errorf("%s not found among Prober nodes", eventSyncer)
	}
	return es.Healthy(ctx)
}
