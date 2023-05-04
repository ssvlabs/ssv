package goclient

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"

	apiv1bellatrix "github.com/attestantio/go-eth2-client/api/v1/bellatrix"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"

	"github.com/bloxapp/ssv/logging/fields"
)

// GetBeaconBlock returns beacon block by the given slot and committee index
func (gc *goClient) GetBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	reqStart := time.Now()
	beaconBlock, err := gc.client.BeaconBlockProposal(gc.ctx, slot, sig, graffiti)
	if err != nil {
		return nil, DataVersionNil, err
	}
	metricsProposerDataRequest.Observe(time.Since(reqStart).Seconds())

	switch beaconBlock.Version {
	case spec.DataVersionPhase0:
		return beaconBlock.Phase0, beaconBlock.Version, nil
	case spec.DataVersionAltair:
		return beaconBlock.Altair, beaconBlock.Version, nil
	case spec.DataVersionBellatrix:
		if b := beaconBlock.Bellatrix.Body; b != nil && b.ExecutionPayload != nil {
			gc.log.Info("got beacon block",
				fields.BlockHash(b.ExecutionPayload.BlockHash),
				fields.BlockVersion(beaconBlock.Version),
				fields.Slot(slot))
		}
		return beaconBlock.Bellatrix, beaconBlock.Version, nil
	case spec.DataVersionCapella:
		if b := beaconBlock.Capella.Body; b != nil && b.ExecutionPayload != nil {
			gc.log.Info("got beacon block",
				fields.BlockHash(b.ExecutionPayload.BlockHash),
				fields.BlockVersion(beaconBlock.Version),
				fields.Slot(slot))
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

func (gc *goClient) GetBlindedBeaconBlock(slot phase0.Slot, graffiti, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	reqStart := time.Now()
	beaconBlock, err := gc.client.BlindedBeaconBlockProposal(gc.ctx, slot, sig, graffiti)
	if err != nil {
		return nil, 0, err
	}
	metricsProposerDataRequest.Observe(time.Since(reqStart).Seconds())

	if beaconBlock == nil {
		return nil, 0, fmt.Errorf("block is nil")
	}

	switch beaconBlock.Version {
	case spec.DataVersionBellatrix:
		if b := beaconBlock.Bellatrix.Body; b != nil && b.ExecutionPayloadHeader != nil {
			gc.log.Info("got blinded beacon block",
				fields.BlockHash(b.ExecutionPayloadHeader.BlockHash),
				fields.BlockVersion(beaconBlock.Version),
				fields.Slot(slot))
		}
		return beaconBlock.Bellatrix, beaconBlock.Version, nil
	case spec.DataVersionCapella:
		if b := beaconBlock.Capella.Body; b != nil && b.ExecutionPayloadHeader != nil {
			gc.log.Info("got blinded beacon block",
				fields.BlockHash(b.ExecutionPayloadHeader.BlockHash),
				fields.BlockVersion(beaconBlock.Version),
				fields.Slot(slot))
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

func (gc *goClient) SubmitBlindedBeaconBlock(block *api.VersionedBlindedBeaconBlock, sig phase0.BLSSignature) error {
	signedBlock := &api.VersionedSignedBlindedBeaconBlock{
		Version: block.Version,
	}
	switch block.Version {
	case spec.DataVersionBellatrix:
		if block.Bellatrix == nil {
			return errors.New("bellatrix blinded block is nil")
		}
		signedBlock.Bellatrix = &apiv1bellatrix.SignedBlindedBeaconBlock{
			Message: block.Bellatrix,
		}
		copy(signedBlock.Bellatrix.Signature[:], sig[:])
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return errors.New("capella blinded block is nil")
		}
		signedBlock.Capella = &apiv1capella.SignedBlindedBeaconBlock{
			Message: block.Capella,
		}
		copy(signedBlock.Capella.Signature[:], sig[:])
	default:
		return errors.New("unknown block version")
	}

	return gc.client.SubmitBlindedBeaconBlock(gc.ctx, signedBlock)
}

// SubmitBeaconBlock submit the block to the node
func (gc *goClient) SubmitBeaconBlock(block *spec.VersionedBeaconBlock, sig phase0.BLSSignature) error {
	signedBlock := &spec.VersionedSignedBeaconBlock{
		Version: block.Version,
	}
	switch block.Version {
	case spec.DataVersionPhase0:
		if block.Phase0 == nil {
			return errors.New("phase0 block is nil")
		}
		signedBlock.Phase0 = &phase0.SignedBeaconBlock{
			Message: block.Phase0,
		}
		copy(signedBlock.Phase0.Signature[:], sig[:])
	case spec.DataVersionAltair:
		if block.Altair == nil {
			return errors.New("altair block is nil")
		}
		signedBlock.Altair = &altair.SignedBeaconBlock{
			Message: block.Altair,
		}
		copy(signedBlock.Altair.Signature[:], sig[:])
	case spec.DataVersionBellatrix:
		if block.Bellatrix == nil {
			return errors.New("bellatrix block is nil")
		}
		signedBlock.Bellatrix = &bellatrix.SignedBeaconBlock{
			Message: block.Bellatrix,
		}
		copy(signedBlock.Bellatrix.Signature[:], sig[:])
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return errors.New("capella block is nil")
		}
		signedBlock.Capella = &capella.SignedBeaconBlock{
			Message: block.Capella,
		}
		copy(signedBlock.Capella.Signature[:], sig[:])
	default:
		return errors.New("unknown block version")
	}

	return gc.client.SubmitBeaconBlock(gc.ctx, signedBlock)
}

func (gc *goClient) SubmitValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	if gc.batchRegistration {
		return gc.submitBatchValidatorRegistration(pubkey, feeRecipient, sig)
	}

	return gc.submitRegularValidatorRegistration(pubkey, feeRecipient, sig)
}

func (gc *goClient) submitRegularValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	pk := phase0.BLSPubKey{}
	copy(pk[:], pubkey)
	signedReg := &api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &eth2apiv1.SignedValidatorRegistration{
			Message: &eth2apiv1.ValidatorRegistration{
				FeeRecipient: feeRecipient,
				// TODO: This is a reasonable default, but we should probably make this configurable.
				//       Discussion here: https://github.com/ethereum/builder-specs/issues/17
				GasLimit:  30_000_000,
				Timestamp: gc.network.GetSlotStartTime(gc.network.GetEpochFirstSlot(gc.network.EstimatedCurrentEpoch())),
				Pubkey:    pk,
			},
			Signature: sig,
		},
	}
	return gc.client.SubmitValidatorRegistrations(gc.ctx, []*api.VersionedSignedValidatorRegistration{signedReg})
}

func (gc *goClient) submitBatchValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	gc.postponedRegistrationsMu.Lock()
	defer gc.postponedRegistrationsMu.Unlock()

	currentSlot := uint64(gc.network.EstimatedCurrentSlot())
	slotsPerEpoch := gc.network.SlotsPerEpoch()

	shouldSubmit := len(gc.postponedRegistrations) != 0 &&
		(currentSlot-gc.lastRegistrationSlot.Load() >= slotsPerEpoch &&
			currentSlot%slotsPerEpoch == gc.operatorID%slotsPerEpoch ||
			currentSlot-gc.lastRegistrationSlot.Load() >= slotsPerEpoch*2+gc.operatorID%slotsPerEpoch)

	if shouldSubmit {
		const batchSize = 500

		for len(gc.postponedRegistrations) > 0 {
			bs := batchSize
			if bs > len(gc.postponedRegistrations) {
				bs = len(gc.postponedRegistrations)
			}

			if err := gc.client.SubmitValidatorRegistrations(gc.ctx, gc.postponedRegistrations[0:bs]); err != nil {
				return err
			}

			gc.log.Info("submitted postponed validator registrations", fields.Count(bs))

			gc.postponedRegistrations = gc.postponedRegistrations[bs:]
		}

		gc.lastRegistrationSlot.Store(currentSlot)

		return nil
	}

	pk := phase0.BLSPubKey{}
	copy(pk[:], pubkey)
	signedReg := &api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &eth2apiv1.SignedValidatorRegistration{
			Message: &eth2apiv1.ValidatorRegistration{
				FeeRecipient: feeRecipient,
				// TODO: This is a reasonable default, but we should probably make this configurable.
				//       Discussion here: https://github.com/ethereum/builder-specs/issues/17
				GasLimit:  30_000_000,
				Timestamp: gc.network.GetSlotStartTime(gc.network.GetEpochFirstSlot(gc.network.EstimatedCurrentEpoch())),
				Pubkey:    pk,
			},
			Signature: sig,
		},
	}

	gc.postponedRegistrations = append(gc.postponedRegistrations, signedReg)

	return nil
}

type ValidatorRegistration struct {
	PubKey       []byte
	FeeRecipient bellatrix.ExecutionAddress
	Sig          phase0.BLSSignature
}

func (gc *goClient) SubmitValidatorRegistrationBatched(pubkeys [][]byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) error {
	registrations := make([]*api.VersionedSignedValidatorRegistration, 0)

	for _, pk := range pubkeys {
		registrations = append(registrations, &api.VersionedSignedValidatorRegistration{
			Version: spec.BuilderVersionV1,
			V1: &eth2apiv1.SignedValidatorRegistration{
				Message: &eth2apiv1.ValidatorRegistration{
					FeeRecipient: feeRecipient,
					// TODO: This is a reasonable default, but we should probably make this configurable.
					//       Discussion here: https://github.com/ethereum/builder-specs/issues/17
					GasLimit:  30_000_000,
					Timestamp: gc.network.GetSlotStartTime(gc.network.GetEpochFirstSlot(gc.network.EstimatedCurrentEpoch())),
					Pubkey:    *(*phase0.BLSPubKey)(pk), // TODO: use phase0.BLSPubKey(pk) in Go 1.20
				},
				Signature: sig,
			},
		})
	}

	return gc.client.SubmitValidatorRegistrations(gc.ctx, registrations)
}

func (gc *goClient) SubmitValidatorRawRegistrations(registrations []*api.VersionedSignedValidatorRegistration) error {
	return gc.client.SubmitValidatorRegistrations(gc.ctx, registrations)
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
