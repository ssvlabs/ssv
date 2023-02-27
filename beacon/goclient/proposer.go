package goclient

import (
	"fmt"
	"github.com/attestantio/go-eth2-client/api"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// GetBeaconBlock returns beacon block by the given slot and committee index
func (gc *goClient) GetBeaconBlock(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, graffiti, randao []byte) (*bellatrix.BeaconBlock, error) {
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

// SubmitBeaconBlock submit the block to the node
func (gc *goClient) SubmitBeaconBlock(block *bellatrix.SignedBeaconBlock) error {
	versionedBlock := &spec.VersionedSignedBeaconBlock{
		Version:   spec.DataVersionBellatrix,
		Bellatrix: block,
	}

	return gc.client.SubmitBeaconBlock(gc.ctx, versionedBlock)
}

func (gc *goClient) GetBlindedBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (*api.VersionedBlindedBeaconBlock, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])
	return gc.client.BlindedBeaconBlockProposal(gc.ctx, slot, sig, graffiti)
}

func (gc *goClient) SubmitBlindedBeaconBlock(block *api.VersionedSignedBlindedBeaconBlock) error {
	return gc.client.SubmitBlindedBeaconBlock(gc.ctx, block)
}

func (gc *goClient) SubmitValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	pk := phase0.BLSPubKey{}
	copy(pk[:], pubkey)
	signedReg := &api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &eth2apiv1.SignedValidatorRegistration{
			Message: &eth2apiv1.ValidatorRegistration{
				FeeRecipient: feeRecipient,
				// From Lighthouse at https://github.com/realbigsean/lighthouse/blob/c7f19354d5b9da257f2416488d131c887ef53640/validator_client/src/preparation_service.rs#L269-L270
				//	TODO(sean) this is geth's default, we should make this configurable and maybe have the default be dynamic.
				//	Discussion here: https://github.com/ethereum/builder-specs/issues/17
				GasLimit:  30_000_000,
				Timestamp: gc.network.GetSlotStartTime(gc.network.GetEpochFirstSlot(gc.network.EstimatedCurrentEpoch())),
				Pubkey:    pk,
			},
			Signature: sig,
		},
	}
	return gc.client.SubmitValidatorRegistrations(gc.ctx, []*api.VersionedSignedValidatorRegistration{signedReg})
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
