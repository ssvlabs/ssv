package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

func (gc *GoClient) SubmitVoluntaryExit(ctx context.Context, voluntaryExit *phase0.SignedVoluntaryExit) error {
	reqStart := time.Now()
	err := gc.multiClient.SubmitVoluntaryExit(ctx, voluntaryExit)
	recordMultiClientRequest(ctx, gc.log, "SubmitVoluntaryExit", http.MethodPost, time.Since(reqStart), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit voluntary exit: %w", err), "SubmitVoluntaryExit")
	}
	return nil
}
