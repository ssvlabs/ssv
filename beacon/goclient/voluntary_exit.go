package goclient

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

func (gc *goClient) SubmitVoluntaryExit(voluntaryExit *phase0.SignedVoluntaryExit, sig phase0.BLSSignature) error {
	return errors.New("not implemented")
}
