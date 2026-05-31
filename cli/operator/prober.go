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

func startHealthProber(ctx context.Context, logger *zap.Logger, p *hprobe.HealthProber) {
	const probeFrequency = 60 * time.Second

	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		func() {
			logger.Debug("health-prober tick: probing all components")
			defer logger.Debug("health-prober tick: probing all components done")

			probeCtx, cancel := context.WithTimeout(ctx, probeFrequency)
			defer cancel()

			if err := p.ProbeAll(probeCtx); err != nil {
				logger.Fatal(componentsUnhealthyErrorMsg, zap.Error(err))
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
