package nodeprobe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// node represents a node being probed.
type node interface {
	Healthy(ctx context.Context) error
}

// pNode allows configuring the max number of retries intended for the node as well as the healthcheckTimeout
// to use when checking for health.
type pNode struct {
	n                  node
	healthcheckTimeout time.Duration
	retriesMax         int
}

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
	// nodes maps node-name to its Node.
	nodes map[string]pNode
}

func NewProber(logger *zap.Logger) *Prober {
	return &Prober{
		logger: logger,
		nodes:  make(map[string]pNode),
	}
}

func (p *Prober) ProbeAll(ctx context.Context) error {
	// Probe all nodes in parallel, use cancel to quit early canceling irrelevant workers.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errsCh := make(chan error)

	p.nodesMu.Lock()
	nodes := p.nodes
	p.nodesMu.Unlock()

	for name, n := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := p.probeNode(ctx, n)
			if err != nil {
				// Relay the error and quit early.
				errsCh <- fmt.Errorf("probe node %s: %w", name, err)
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

func (p *Prober) Probe(ctx context.Context, nodeName string) error {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	node, ok := p.nodes[nodeName]
	if !ok {
		return fmt.Errorf("%s not found among Prober nodes", nodeName)
	}

	err := p.probeNode(ctx, node)
	if err != nil {
		return fmt.Errorf("probe node %s: %w", nodeName, err)
	}

	return nil
}

func (p *Prober) probeNode(ctx context.Context, node pNode) (err error) {
	defer func() {
		// Catch panics to present these (however unlikely they are) as a readable error-message.
		if e := recover(); e != nil {
			err = fmt.Errorf("panic: %v", e)
		}
	}()

	// Retry health-check multiple times to make sure we do not classify an occasional glitch (or a network blip)
	// as node being unhealthy. Failing on the very 1st failed request would be too drastic a measure given it
	// may result into SSV node restart.
	for attempt := 0; attempt <= node.retriesMax; attempt++ {
		err = func() error {
			healthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			return node.n.Healthy(healthCtx)
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

	return fmt.Errorf("node is unhealthy: %w", err)
}

func (p *Prober) AddNode(nodeName string, node node, healthcheckTimeout time.Duration, retriesMax int) {
	p.nodesMu.Lock()
	defer p.nodesMu.Unlock()

	p.nodes[nodeName] = pNode{
		n:                  node,
		healthcheckTimeout: healthcheckTimeout,
		retriesMax:         retriesMax,
	}
}
