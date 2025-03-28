package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

func (gc *GoClient) ForkAtEpoch(ctx context.Context, epoch phase0.Epoch) (*phase0.Fork, error) {
	start := time.Now()
	// ForkSchedule result is cached in the client and updated once in a while.
	// So calling this method often shouldn't worsen the performance.
	schedule, err := gc.multiClient.ForkSchedule(ctx, &api.ForkScheduleOpts{})
	recordRequestDuration(gc.ctx, "ForkSchedule", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "ForkSchedule"),
			zap.Error(err),
		)
		return nil, err
	}
	if schedule.Data == nil {
		gc.log.Error(clNilResponseForkDataErrMsg,
			zap.String("api", "ForkSchedule"),
		)
		return nil, fmt.Errorf("fork schedule response data is nil")
	}

	var forkAtEpoch *phase0.Fork
	for _, fork := range schedule.Data {
		if fork.Epoch <= epoch && (forkAtEpoch == nil || fork.Epoch > forkAtEpoch.Epoch) {
			forkAtEpoch = fork
		}
	}

	if forkAtEpoch == nil {
		return nil, fmt.Errorf("could not find fork at epoch %d", epoch)
	}

	return forkAtEpoch, nil
}
