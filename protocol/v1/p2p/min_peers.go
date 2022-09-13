package protcolp2p

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
	"time"
)

// WaitForMinPeers waits until there are minPeers conntected for the given validator
func WaitForMinPeers(pctx context.Context, logger *zap.Logger, subscriber Subscriber, vpk spectypes.ValidatorPK, minPeers int, interval time.Duration) error {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	tries := 0
	for ctx.Err() == nil {
		tries++
		time.Sleep(interval)
		peers, err := subscriber.Peers(vpk)
		if err != nil {
			logger.Warn("could not get peers of topic", zap.Error(err))
			continue
		}
		if len(peers) >= minPeers {
			return nil
		}
		if tries%10 == 0 { // after 10 times, try to find relevant peer
			count, _ := subscriber.ConnectPeers(vpk, minPeers, time.Minute*2)
			if count >= minPeers {
				return nil
			}
		}
	}

	return ctx.Err()
}
