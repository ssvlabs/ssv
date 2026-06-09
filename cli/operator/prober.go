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

func ensureComponentsHealthy(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := p.ProbeAll(probeCtx); err != nil {
		return fmt.Errorf("%s: %w", componentsUnhealthyErrorMsg, err)
	}

	logger.Info("all component(s) are healthy")
	return nil
}

// startHealthProber is a runtime liveness watchdog: it probes all components every probeFrequency
// and, on persistent unhealth, returns an error so the node terminates gracefully (via Close,
// non-zero exit) and the orchestrator restarts it; a clean ctx cancellation returns nil. Crashing on
// persistent unhealth is intentional — a node idling with a dead EL while reporting healthy is a
// silent failure, worse than a restart — so it must not degrade to log-and-retry.
func startHealthProber(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	const (
		probeFrequency = 60 * time.Second // how often a probe round runs
		probeTimeout   = 60 * time.Second // max time for one probe round (ProbeAll)
	)

	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		logger.Debug("health-prober tick: probing all components")
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		err := p.ProbeAll(probeCtx)
		cancel() // not deferred: this is a loop, so release each tick's ctx immediately
		if err != nil {
			// Key off the parent ctx, not the error value: a wedged component (e.g. a hung EL) surfaces
			// as a probe DeadlineExceeded while ctx is still live and must trip the watchdog, so
			// matching context.Canceled/DeadlineExceeded would suppress real unhealth. A canceled
			// parent ctx means a deliberate stop (shutdown, or a sibling already failed), so nil is safe.
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
