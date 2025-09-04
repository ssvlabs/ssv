package operator

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/nodeprobe"
)

const (
	clNodeName          = "consensus client"
	elNodeName          = "execution client"
	eventSyncerNodeName = "event-syncer"
)

func startNodeProber(ctx context.Context, logger *zap.Logger, p *nodeprobe.Prober) {
	const probeFrequency = 60 * time.Second

	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	for {
		func() {
			probeCtx, cancel := context.WithTimeout(ctx, probeFrequency)
			defer cancel()

			if err := p.ProbeAll(probeCtx); err != nil {
				logger.Fatal("Ethereum node(s) are either out of sync or down. Ensure the nodes are healthy to resume.", zap.Error(err))
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
