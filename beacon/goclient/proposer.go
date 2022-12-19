package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// GetBeaconBlock returns beacon block by the given slot and committee index
func (gc *goClient) GetBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (*bellatrix.BeaconBlock, error) {
	if provider, isProvider := gc.client.(eth2client.BeaconBlockProposalProvider); isProvider {
		// TODO need to support blinded?
		// TODO what with fee recipient?
		sig := phase0.BLSSignature{}
		copy(sig[:], randao[:])

		beaconBlockRoot, err := provider.BeaconBlockProposal(gc.ctx, slot, sig, graffiti)
		if err != nil {
			return nil, err
		}
		return beaconBlockRoot.Bellatrix, nil
	}
	return nil, errors.New("client does not support BeaconBlockProposalProvider")
}

// SubmitBeaconBlock submit the block to the node
func (gc *goClient) SubmitBeaconBlock(block *bellatrix.SignedBeaconBlock) error {
	if provider, isProvider := gc.client.(eth2client.BeaconBlockSubmitter); isProvider {
		versionedBlock := &spec.VersionedSignedBeaconBlock{
			Version:   spec.DataVersionBellatrix,
			Bellatrix: block,
		}

		// TODO check slashable event?
		return provider.SubmitBeaconBlock(gc.ctx, versionedBlock)
	}
	return errors.New("client does not support BeaconBlockSubmitter")
}
