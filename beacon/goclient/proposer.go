package goclient

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1bellatrix "github.com/attestantio/go-eth2-client/api/v1/bellatrix"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/logging/fields"
)

const (
	batchSize = 500
	// ValidatorRegistrationGasLimit sets validator registration gas limit.
	// TODO: This is a reasonable default, but we should probably make this configurable.
	//       Discussion here: https://github.com/ethereum/builder-specs/issues/17
	ValidatorRegistrationGasLimit = 30_000_000
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

	if beaconBlock == nil {
		return nil, 0, fmt.Errorf("block is nil")
	}

	switch beaconBlock.Version {
	case spec.DataVersionPhase0:
		return beaconBlock.Phase0, beaconBlock.Version, nil
	case spec.DataVersionAltair:
		return beaconBlock.Altair, beaconBlock.Version, nil
	case spec.DataVersionBellatrix:
		b := beaconBlock.Bellatrix.Body
		if b == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block body is nil")
		}

		if b.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block execution payload is nil")
		}

		gc.logBlock(slot, b.ExecutionPayload.BlockHash, beaconBlock.Version, false)

		return beaconBlock.Bellatrix, beaconBlock.Version, nil
	case spec.DataVersionCapella:
		b := beaconBlock.Capella.Body
		if b == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}

		if b.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload is nil")
		}

		gc.logBlock(slot, b.ExecutionPayload.BlockHash, beaconBlock.Version, false)

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
		return nil, 0, fmt.Errorf("blinded block is nil")
	}

	switch beaconBlock.Version {
	case spec.DataVersionBellatrix:
		b := beaconBlock.Bellatrix.Body
		if b == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block body is nil")
		}

		if b.ExecutionPayloadHeader == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block execution payload header is nil")
		}

		gc.logBlock(slot, b.ExecutionPayloadHeader.BlockHash, beaconBlock.Version, true)

		return beaconBlock.Bellatrix, beaconBlock.Version, nil
	case spec.DataVersionCapella:
		b := beaconBlock.Capella.Body
		if b == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}

		if b.ExecutionPayloadHeader == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload header is nil")
		}

		gc.logBlock(slot, b.ExecutionPayloadHeader.BlockHash, beaconBlock.Version, true)

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
	currentSlot := uint64(gc.network.EstimatedCurrentSlot())
	slotsPerEpoch := gc.network.SlotsPerEpoch()
	slotsSinceLastRegistration := currentSlot - gc.registrationLastSlot.Load()
	operatorSubmissionSlotModulo := gc.operatorID % slotsPerEpoch

	operatorSubmissionSlot := currentSlot%slotsPerEpoch == operatorSubmissionSlotModulo
	oneEpochPassed := slotsSinceLastRegistration >= slotsPerEpoch
	twoEpochsAndOperatorDelayPassed := slotsSinceLastRegistration >= slotsPerEpoch*2+operatorSubmissionSlotModulo

	shouldSubmit := gc.hasRegistrations() &&
		(oneEpochPassed && operatorSubmissionSlot || twoEpochsAndOperatorDelayPassed)

	if shouldSubmit {
		return gc.submitBatchedRegistrations(currentSlot)
	}

	gc.enqueueBatchRegistration(pubkey, feeRecipient, sig)

	return nil
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

func (gc *goClient) submitBatchedRegistrations(currentSlot uint64) error {
	for gc.hasRegistrations() {
		nextChunk := gc.getNextRegistrationsChunk()

		if err := gc.client.SubmitValidatorRegistrations(gc.ctx, nextChunk); err != nil {
			return err
		}

		gc.log.Info("submitted batch validator registrations", fields.Count(len(nextChunk)))

		gc.removeSubmittedRegistrations(len(nextChunk))
	}

	gc.registrationLastSlot.Store(currentSlot)

	return nil
}

func (gc *goClient) enqueueBatchRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) {
	registration := gc.createValidatorRegistration(pubkey, feeRecipient, sig)

	gc.registrationsMu.Lock()
	defer gc.registrationsMu.Unlock()

	gc.registrations = append(gc.registrations, registration)
}

func (gc *goClient) createValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) *api.VersionedSignedValidatorRegistration {
	pk := phase0.BLSPubKey{}
	copy(pk[:], pubkey)

	signedReg := &api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &eth2apiv1.SignedValidatorRegistration{
			Message: &eth2apiv1.ValidatorRegistration{
				FeeRecipient: feeRecipient,
				GasLimit:     ValidatorRegistrationGasLimit,
				Timestamp:    gc.network.GetSlotStartTime(gc.network.GetEpochFirstSlot(gc.network.EstimatedCurrentEpoch())),
				Pubkey:       pk,
			},
			Signature: sig,
		},
	}
	return signedReg
}

func (gc *goClient) hasRegistrations() bool {
	gc.registrationsMu.Lock()
	defer gc.registrationsMu.Unlock()

	return len(gc.registrations) != 0
}

func (gc *goClient) getNextRegistrationsChunk() []*api.VersionedSignedValidatorRegistration {
	gc.registrationsMu.Lock()
	defer gc.registrationsMu.Unlock()

	bs := batchSize
	if bs > len(gc.registrations) {
		bs = len(gc.registrations)
	}

	return gc.registrations[0:bs]
}

func (gc *goClient) removeSubmittedRegistrations(count int) {
	gc.registrationsMu.Lock()
	defer gc.registrationsMu.Unlock()

	gc.registrations = gc.registrations[count:]
}

func (gc *goClient) logBlock(slot phase0.Slot, hash phase0.Hash32, version spec.DataVersion, blinded bool) {
	msg := "got beacon block"
	if blinded {
		msg = "got blinded beacon block"
	}
	gc.log.Info(msg,
		fields.BlockHash(hash),
		fields.BlockVersion(version),
		fields.Slot(slot))
}
