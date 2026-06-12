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

// ProbeAll probes all components in parallel and returns the joined failure verdict (nil if no
// failure was observed). It returns when every probe finishes or when ctx expires, whichever comes
// first — a Healthy impl that ignores its ctx can't stall the round past its deadline.
func (p *HealthProber) ProbeAll(ctx context.Context) error {
	// The internal cancel quits the remaining probes early once one component has failed.
	probeCtx, probeCancel := context.WithCancel(ctx)
	defer probeCancel()

	var wg sync.WaitGroup
	// Buffered to component count so a worker never blocks on send: it can deposit its error and
	// exit even after ProbeAll has already returned on ctx expiry below.
	errsCh := make(chan error, p.components.SlowLen())

	p.components.Range(func(name string, n pComponent) bool {
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := p.probeComponent(probeCtx, n)
			if err != nil {
				// Relay the failure and quit the other probes early.
				errsCh <- fmt.Errorf("probe component %s: %w", name, err)
				probeCancel()
			}
		}()
		return true
	})

	go func() {
		wg.Wait()
		close(errsCh)
	}()

	// Collect failures until every worker returns (errsCh closed) or ctx ends the round. A timeout is
	// itself a failure and returns straight away; the other two exits fall through to the verdict below.
	var errs error
collect:
	for {
		select {
		case err, ok := <-errsCh:
			if !ok {
				break collect // every worker returned
			}
			errs = errors.Join(errs, err)
		case <-ctx.Done():
			// Don't wait out a wedged worker (a Healthy impl ignoring its ctx): report what's known.
			// A wedged probe goroutine is unreapable — it leaks until process exit.
			if !errors.Is(ctx.Err(), context.Canceled) {
				return fmt.Errorf("probe health-check timed out: %w", errors.Join(errs, ctx.Err()))
			}
			break collect // cancellation isn't a verdict — report only the real failures, if any
		}
	}

	// Verdict: the joined component failures, or nil if none were observed.
	if errs != nil {
		return fmt.Errorf("probe health-check failed: %w", errs)
	}
	return nil
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
			return nil // probing was cut short (canceled) — not a component failure
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
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil // probing was cut short (canceled) — not a component failure
			}
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
