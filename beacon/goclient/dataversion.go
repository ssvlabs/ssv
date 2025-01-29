package goclient

import (
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

func (gc *GoClient) DataVersion(epoch phase0.Epoch) spec.DataVersion {
	// TODO

	return spec.DataVersionElectra
}
