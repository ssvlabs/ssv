package goclient

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

func (gc *goClient) SubmitVoluntaryExit(voluntaryExit *phase0.SignedVoluntaryExit) error {
	return gc.client.SubmitVoluntaryExit(gc.ctx, voluntaryExit)
}
