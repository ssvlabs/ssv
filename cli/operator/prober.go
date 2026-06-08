package operator

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/hprobe"
)

// List of health-prober components.
const (
	clComponentName          = "consensus client"
	elComponentName          = "execution client"
	eventSyncerComponentName = "event-syncer"
	p2pComponentName         = "p2p"
)

// Common prober parameters we use to health check various prober-components.
const (
	proberHealthcheckTimeout = 10 * time.Second
	proberRetriesMax         = 5
	proberRetryDelay         = 10 * time.Second
)

const componentsUnhealthyErrorMsg = "component(s) are not healthy"

// probe runs a single ProbeAll over all registered components, bounded by timeout, and returns the
// prober's error verbatim. It is the shared core of the one-shot startup gate (ensureComponentsHealthy)
// and the periodic runtime watchdog (startHealthProber), which differ only in timeout and the policy
// wrapped around it — fail-fast vs. shutdown-trip.
func probe(ctx context.Context, p *hprobe.HealthProber, timeout time.Duration) error {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return p.ProbeAll(probeCtx)
}

func ensureComponentsHealthy(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	if err := probe(ctx, p, 30*time.Second); err != nil {
		return fmt.Errorf("%s: %w", componentsUnhealthyErrorMsg, err)
	}

	logger.Info("all component(s) are healthy")
	return nil
}

// startHealthProber is a runtime liveness watchdog: every probeFrequency it probes all components,
// and on persistent unhealth returns an error so the node terminates gracefully (errgroup -> Close ->
// non-zero exit) and the orchestrator restarts it — potentially onto a healthy endpoint. A clean ctx
// cancellation (normal shutdown) returns nil. The crash-and-restart on persistent unhealth is
// intentional: a node idling with a dead EL while reporting healthy at the process level is a silent
// failure, worse than a loud restart — so this must not degrade to "log and retry forever".
func startHealthProber(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	const probeFrequency = 60 * time.Second

	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		logger.Debug("health-prober tick: probing all components")
		if err := probe(ctx, p, probeFrequency); err != nil {
			// A canceled ctx means we're shutting down, not that a component is unhealthy: the probe
			// inherits ctx, so cancellation surfaces here as a probe error. Return nil (exit 0) rather
			// than tripping the watchdog (non-zero exit).
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("%s: %w", componentsUnhealthyErrorMsg, err)
		}
		logger.Debug("health-prober tick: probing all components done")

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
