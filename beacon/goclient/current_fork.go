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

func (gc *GoClient) CurrentFork(ctx context.Context) (*phase0.Fork, error) {
	start := time.Now()
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

	currentEpoch := gc.network.EstimatedCurrentEpoch()
	var currentFork *phase0.Fork
	for _, fork := range schedule.Data {
		if fork.Epoch <= currentEpoch && (currentFork == nil || fork.Epoch > currentFork.Epoch) {
			currentFork = fork
		}
	}

	if currentFork == nil {
		return nil, fmt.Errorf("could not find current fork")
	}

	return currentFork, nil
}
