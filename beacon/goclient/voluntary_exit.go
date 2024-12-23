package goclient

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

func (gc *GoClient) SubmitVoluntaryExit(voluntaryExit *phase0.SignedVoluntaryExit) error {
	if err := gc.multiClient.SubmitVoluntaryExit(gc.ctx, voluntaryExit); err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "SubmitVoluntaryExit"),
			zap.Error(err),
		)
		return err
	}

	return nil
}
