package goclient

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// SubmitBeaconCommitteeSubscriptions is implementation for subscribing committee to subnet (p2p topic)
func (gc *GoClient) SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error {
	logger := gc.log.With(zap.String("api", "SubmitBeaconCommitteeSubscriptions"))

	var submissions int32
	p := pool.New().WithErrors().WithContext(gc.ctx).WithMaxGoroutines(len(gc.clients))
	for _, client := range gc.clients {
		client := client
		p.Go(func(ctx context.Context) error {
			clientAddress := client.Address()
			logger := logger.With(zap.String("client_addr", clientAddress))

			start := time.Now()
			err := client.SubmitBeaconCommitteeSubscriptions(ctx, subscription)
			recordRequestDuration(gc.ctx, "SubmitBeaconCommitteeSubscriptions", clientAddress, http.MethodPost, time.Since(start), err)

			if err != nil {
				logger.Error("client failed to submit beacon committee subscriptions",
					zap.Error(err))
				return fmt.Errorf("client failed to submit beacon committee subscriptions (%s): %w", clientAddress, err)
			}

			logger.Debug("client submitted beacon committee subscriptions", zap.Duration("duration", time.Since(start)))
			atomic.AddInt32(&submissions, 1)
			return nil
		})
	}
	err := p.Wait()
	if atomic.LoadInt32(&submissions) > 0 {
		// At least one client has submitted the subscriptions successfully,
		// so we can return without error.
		return nil
	}
	if err != nil {
		logger.Error("failed to submit beacon committee subscriptions",
			zap.Error(err))
		return fmt.Errorf("failed to submit beacon committee subscriptions")
	}
	return nil
}

// SubmitSyncCommitteeSubscriptions is implementation for subscribing sync committee to subnet (p2p topic)
func (gc *GoClient) SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error {
	start := time.Now()
	err := gc.multiClient.SubmitSyncCommitteeSubscriptions(ctx, subscription)
	recordRequestDuration(gc.ctx, "SubmitSyncCommitteeSubscriptions", gc.multiClient.Address(), http.MethodPost, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitSyncCommitteeSubscriptions"),
			zap.Error(err),
		)
	}

	return err
}
