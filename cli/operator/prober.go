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

// Health-check parameters shared by the prober components. Probing one component is an initial
// attempt plus proberRetriesMax retries, each capped at proberHealthcheckTimeout and spaced by
// proberRetryDelay — so a component stuck timing out takes ~110s (6 attempts x 10s + 5 gaps x 10s)
// to be declared unhealthy.
const (
	proberHealthcheckTimeout = 10 * time.Second
	proberRetriesMax         = 5
	proberRetryDelay         = 10 * time.Second
)

const componentsUnhealthyErrorMsg = "component(s) are not healthy"

func ensureComponentsHealthy(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	// gateTimeout cuts the full per-component probe schedule (~110s; see the prober consts) short at
	// bring-up: a component should answer within a couple of attempts, and a failed gate just gets the
	// node restarted.
	const gateTimeout = 30 * time.Second

	probeCtx, cancel := context.WithTimeout(ctx, gateTimeout)
	defer cancel()

	if err := p.ProbeAll(probeCtx); err != nil {
		// A canceled parent ctx is a deliberate stop (eg. shutdown), everything else fails the gate.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: %w", componentsUnhealthyErrorMsg, err)
	}

	logger.Info("all component(s) are healthy")
	return nil
}

// startHealthProber is a runtime liveness watchdog: it probes all components every probeInterval
// and, on persistent unhealth, returns an error so the node terminates gracefully (via Close,
// non-zero exit) and the orchestrator restarts it; a clean ctx cancellation returns nil. Crashing on
// persistent unhealth is intentional — a node idling with a dead EL while reporting healthy is a
// silent failure, worse than a restart — so it must not degrade to log-and-retry.
func startHealthProber(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	const (
		// probeInterval is the pause between probe rounds, measured from round end — a round that runs
		// long (components burning retries before recovering) still leaves a full quiet gap after it,
		// never back-to-back rounds.
		probeInterval = 60 * time.Second
		// probeTimeout bounds one round (ProbeAll) as the watchdog's own liveness guard: even a
		// Healthy impl that ignores its ctx can't stall the loop. Like the bring-up gate's timeout,
		// it deliberately cuts the full probe schedule short — failing solidly for a whole round is
		// exactly the unhealth the watchdog exists to trip on.
		probeTimeout = 60 * time.Second
	)

	for {
		logger.Debug("health-prober tick: probing all components")
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		err := p.ProbeAll(probeCtx)
		cancel() // not deferred: this is a loop, so release each round's ctx immediately
		if err != nil {
			// A canceled parent ctx is a deliberate stop (eg. shutdown), everything else trips the watchdog.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("%s: %w", componentsUnhealthyErrorMsg, err)
		}
		logger.Debug("health-prober tick: probing all components done")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(probeInterval):
		}
	}
}
