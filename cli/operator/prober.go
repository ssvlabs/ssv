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

func startHealthProber(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) error {
	const probeFrequency = 60 * time.Second

	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		logger.Debug("health-prober tick: probing all components")
		probeCtx, cancel := context.WithTimeout(ctx, probeFrequency)
		err := p.ProbeAll(probeCtx)
		cancel()
		logger.Debug("health-prober tick: probing all components done")
		if err != nil {
			return fmt.Errorf("%s: %w", componentsUnhealthyErrorMsg, err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
