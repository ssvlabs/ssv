package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	"go.uber.org/zap"
)

func (gc *GoClient) Spec(ctx context.Context) (map[string]any, error) {
	return specImpl(ctx, gc.log, gc.multiClient)
}

// It's used in both Spec and singleClientHooks, so we need some common implementation to avoid code repetition.
func specImpl(ctx context.Context, log *zap.Logger, provider client.Service) (map[string]any, error) {
	start := time.Now()
	specResponse, err := provider.(client.SpecProvider).Spec(ctx, &api.SpecOpts{})
	recordRequestDuration(ctx, "Spec", provider.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		log.Error(clResponseErrMsg,
			zap.String("api", "Spec"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain spec response: %w", err)
	}
	if specResponse == nil {
		log.Error(clNilResponseErrMsg,
			zap.String("api", "Spec"),
		)
		return nil, fmt.Errorf("spec response is nil")
	}
	if specResponse.Data == nil {
		log.Error(clNilResponseDataErrMsg,
			zap.String("api", "Spec"),
		)
		return nil, fmt.Errorf("spec response data is nil")
	}

	return specResponse.Data, nil
}
