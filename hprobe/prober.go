package hprobe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/utils/hashmap"
)

// component is an interface a component needs to implement for HealthProber to be able to probe it.
type component interface {
	Healthy(ctx context.Context) error
}

// pComponent represents a component being probed, allows configuring the max number of retries intended for the
// component as well as the healthcheckTimeout to use when checking for health.
type pComponent struct {
	name string
	c    component

	healthcheckTimeout time.Duration
	retriesMax         int
	retryDelay         time.Duration
}

// HealthProber allows for probing (checking the health of) the components it is configured with. It supports retries
// that are useful for tolerating occasional failures.
type HealthProber struct {
	logger *zap.Logger

	// components maps component-name to its Component.
	components *hashmap.Map[string, pComponent]
}

func NewHealthProber(logger *zap.Logger) *HealthProber {
	return &HealthProber{
		logger:     logger,
		components: hashmap.New[string, pComponent](),
	}
}

func (p *HealthProber) ProbeAll(ctx context.Context) error {
	// Probe all components in parallel, use cancel to quit early canceling irrelevant workers.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errsCh := make(chan error)

	p.components.Range(func(name string, n pComponent) bool {
		wg.Go(func() {
			err := p.probeComponent(ctx, n)
			if err != nil {
				// Relay the error and quit early.
				errsCh <- fmt.Errorf("probe component %s: %w", name, err)
				cancel()
			}
		})
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

func (p *HealthProber) Probe(ctx context.Context, componentName string) error {
	n, ok := p.components.Get(componentName)
	if !ok {
		return fmt.Errorf("%s not found among Prober components", componentName)
	}

	err := p.probeComponent(ctx, n)
	if err != nil {
		return fmt.Errorf("probe component %s: %w", componentName, err)
	}

	return nil
}

func (p *HealthProber) probeComponent(ctx context.Context, c pComponent) (err error) {
	// Retry health-check multiple times to make sure we do not classify an occasional glitch (or a network blip)
	// as component being unhealthy. Failing on the very 1st failed request would be too drastic a measure given it
	// may result into SSV component restart.
	attemptsMax := 1 + c.retriesMax // the initial attempt + retries specified
	for attempt := 1; attempt <= attemptsMax; attempt++ {
		err = func() error {
			healthCtx, cancel := context.WithTimeout(ctx, c.healthcheckTimeout)
			defer cancel()

			return c.c.Healthy(healthCtx)
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
			zap.String("component", c.name),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)

		// Wait before the next attempt.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.retryDelay):
		}
	}

	return fmt.Errorf("component is unhealthy: %w", err)
}

func (p *HealthProber) AddComponent(componentName string, component component, healthcheckTimeout time.Duration, retriesMax int, retryDelay time.Duration) {
	p.components.Set(componentName, pComponent{
		name:               componentName,
		c:                  component,
		healthcheckTimeout: healthcheckTimeout,
		retriesMax:         retriesMax,
		retryDelay:         retryDelay,
	})
}
