package operator

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/nodeprobe"
)

// List of prober prober-nodes.
const (
	clNodeName          = "consensus client"
	elNodeName          = "execution client"
	eventSyncerNodeName = "event-syncer"
)

// Common prober parameters we use for various prober-nodes.
const (
	proberHealthcheckTimeout = 10 * time.Second
	proberRetriesMax         = 5
	proberRetryDelay         = 10 * time.Second
)

const ethereumNodesUnhealthyFatalErrorMsg = "ethereum node(s) are not healthy"

func ensureEthereumNodesHealthy(ctx context.Context, logger *zap.Logger, p *nodeprobe.Prober) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := p.ProbeAll(probeCtx); err != nil {
		logger.Fatal(ethereumNodesUnhealthyFatalErrorMsg, zap.Error(err))
	}

	logger.Info("ethereum node(s) are healthy")
}

func startNodeProber(ctx context.Context, logger *zap.Logger, p *nodeprobe.Prober) {
	const probeFrequency = 60 * time.Second

	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		func() {
			logger.Debug("node-prober tick: probing all nodes")
			defer logger.Debug("node-prober tick: probing all nodes done")

			probeCtx, cancel := context.WithTimeout(ctx, probeFrequency)
			defer cancel()

			if err := p.ProbeAll(probeCtx); err != nil {
				logger.Fatal(ethereumNodesUnhealthyFatalErrorMsg, zap.Error(err))
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
