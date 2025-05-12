package goclient

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

func (gc *GoClient) SubmitVoluntaryExit(voluntaryExit *phase0.SignedVoluntaryExit) error {
	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitVoluntaryExit"),
		zap.String("client_addr", clientAddress))

	if err := gc.multiClient.SubmitVoluntaryExit(gc.ctx, voluntaryExit); err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted voluntary exit")
	return nil
}
