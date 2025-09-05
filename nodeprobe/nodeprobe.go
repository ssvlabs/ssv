package nodeprobe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/utils/hashmap"
)

// node represents a node being probed.
type node interface {
	Healthy(ctx context.Context) error
}

// pNode allows configuring the max number of retries intended for the node as well as the healthcheckTimeout
// to use when checking for health.
type pNode struct {
	name string
	n    node

	healthcheckTimeout time.Duration
	retriesMax         int
}

// Prober allows for probing (checking the health of) the nodes it is configured with. It supports retries
// that are useful for tolerating occasional failures.
type Prober struct {
	logger *zap.Logger

	// nodes maps node-name to its Node.
	nodes *hashmap.Map[string, pNode]
}

func New(logger *zap.Logger) *Prober {
	return &Prober{
		logger: logger,
		nodes:  hashmap.New[string, pNode](),
	}
}

func (p *Prober) ProbeAll(ctx context.Context) error {
	// Probe all nodes in parallel, use cancel to quit early canceling irrelevant workers.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errsCh := make(chan error)

	p.nodes.Range(func(name string, n pNode) bool {
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
		return true
	})

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
	n, ok := p.nodes.Get(nodeName)
	if !ok {
		return fmt.Errorf("%s not found among Prober nodes", nodeName)
	}

	err := p.probeNode(ctx, n)
	if err != nil {
		return fmt.Errorf("probe node %s: %w", nodeName, err)
	}

	return nil
}

func (p *Prober) probeNode(ctx context.Context, n pNode) (err error) {
	// Retry health-check multiple times to make sure we do not classify an occasional glitch (or a network blip)
	// as node being unhealthy. Failing on the very 1st failed request would be too drastic a measure given it
	// may result into SSV node restart.
	attemptsMax := 1 + n.retriesMax
	for attempt := 1; attempt <= attemptsMax; attempt++ {
		err = func() error {
			healthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			return n.n.Healthy(healthCtx)
		}()
		if errors.Is(err, context.Canceled) {
			return nil // probing was canceled (it's not an error then)
		}
		if err == nil {
			return nil // success
		}
		if attempt == attemptsMax {
			break // all retries failed
		}

		p.logger.Debug("health-check failed, gonna retry",
			zap.String("node", n.name),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)
	}

	return fmt.Errorf("node is unhealthy: %w", err)
}

func (p *Prober) AddNode(nodeName string, node node, healthcheckTimeout time.Duration, retriesMax int) {
	p.nodes.Set(nodeName, pNode{
		name:               nodeName,
		n:                  node,
		healthcheckTimeout: healthcheckTimeout,
		retriesMax:         retriesMax,
	})
}
