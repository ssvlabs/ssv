package goclient

import (
	"fmt"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1bellatrix "github.com/attestantio/go-eth2-client/api/v1/bellatrix"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// GetBeaconBlock returns beacon block by the given slot and committee index
func (gc *goClient) GetBeaconBlock(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, graffiti, randao []byte) (*bellatrix.BeaconBlock, error) {
	// TODO need to support blinded?
	// TODO what with fee recipient?
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	reqStart := time.Now()
	beaconBlockRoot, err := gc.client.BeaconBlockProposal(gc.ctx, slot, sig, graffiti)
	if err != nil {
		return nil, err
	}
	metricsProposerDataRequest.Observe(time.Since(reqStart).Seconds())

	switch beaconBlockRoot.Version {
	case spec.DataVersionBellatrix:
		return beaconBlockRoot.Bellatrix, nil
	default:
		return nil, errors.New(fmt.Sprintf("beacon block version %s not supported", beaconBlockRoot.Version))
	}
}

func (gc *goClient) GetBlindedBeaconBlock(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, graffiti, randao []byte) (*apiv1bellatrix.BlindedBeaconBlock, error) {
	return nil, nil
}

func (gc *goClient) SubmitBlindedBeaconBlock(block *apiv1bellatrix.SignedBlindedBeaconBlock) error {
	return nil
}

// SubmitBeaconBlock submit the block to the node
func (gc *goClient) SubmitBeaconBlock(block *bellatrix.SignedBeaconBlock) error {
	versionedBlock := &spec.VersionedSignedBeaconBlock{
		Version:   spec.DataVersionBellatrix,
		Bellatrix: block,
	}

	return gc.client.SubmitBeaconBlock(gc.ctx, versionedBlock)
}

func (gc *goClient) SubmitProposalPreparation(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
	var preparations []*eth2apiv1.ProposalPreparation
	for index, recipient := range feeRecipients {
		preparations = append(preparations, &eth2apiv1.ProposalPreparation{
			ValidatorIndex: index,
			FeeRecipient:   recipient,
		})
	}
	return gc.client.SubmitProposalPreparations(gc.ctx, preparations)
}
