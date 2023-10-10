package goclient

import (
	"context"
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
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/operator/slotticker"
)

const (
	batchSize = 500
)

// ProposerDuties returns proposer duties for the given epoch.
func (gc *goClient) ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	return gc.client.ProposerDuties(ctx, epoch, validatorIndices)
}

// GetBeaconBlock returns beacon block by the given slot, graffiti, and randao.
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
		if beaconBlock.Bellatrix.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block body is nil")
		}
		if beaconBlock.Bellatrix.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block execution payload is nil")
		}
		return beaconBlock.Bellatrix, beaconBlock.Version, nil
	case spec.DataVersionCapella:
		if beaconBlock.Capella.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}
		if beaconBlock.Capella.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload is nil")
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

// GetBlindedBeaconBlock returns blinded beacon block by the given slot, graffiti, and randao.
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
		if beaconBlock.Bellatrix.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block body is nil")
		}
		if beaconBlock.Bellatrix.Body.ExecutionPayloadHeader == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix block execution payload header is nil")
		}
		return beaconBlock.Bellatrix, beaconBlock.Version, nil
	case spec.DataVersionCapella:
		if beaconBlock.Capella.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}
		if beaconBlock.Capella.Body.ExecutionPayloadHeader == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload header is nil")
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
	return gc.updateBatchRegistrationCache(gc.createValidatorRegistration(pubkey, feeRecipient, sig))
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

func (gc *goClient) updateBatchRegistrationCache(registration *api.VersionedSignedValidatorRegistration) error {
	pk, err := registration.PubKey()
	if err != nil {
		return err
	}

	gc.registrationMu.Lock()
	defer gc.registrationMu.Unlock()

	gc.registrationCache[pk] = registration
	return nil
}

func (gc *goClient) createValidatorRegistration(pubkey []byte, feeRecipient bellatrix.ExecutionAddress, sig phase0.BLSSignature) *api.VersionedSignedValidatorRegistration {
	pk := phase0.BLSPubKey{}
	copy(pk[:], pubkey)

	signedReg := &api.VersionedSignedValidatorRegistration{
		Version: spec.BuilderVersionV1,
		V1: &eth2apiv1.SignedValidatorRegistration{
			Message: &eth2apiv1.ValidatorRegistration{
				FeeRecipient: feeRecipient,
				GasLimit:     gc.gasLimit,
				Timestamp:    gc.network.GetSlotStartTime(gc.network.GetEpochFirstSlot(gc.network.EstimatedCurrentEpoch())),
				Pubkey:       pk,
			},
			Signature: sig,
		},
	}
	return signedReg
}

func (gc *goClient) registrationSubmitter(slotTickerProvider slotticker.Provider) {
	ticker := slotTickerProvider()
	for {
		select {
		case <-gc.ctx.Done():
			return
		case <-ticker.Next():
			gc.submitRegistrationsFromCache(ticker.Slot())
		}
	}
}

func (gc *goClient) submitRegistrationsFromCache(currentSlot phase0.Slot) {
	slotsPerEpoch := gc.network.SlotsPerEpoch()

	// Lock:
	// - getting and updating last slot to avoid multiple submission (both should be an atomic action but cannot be done with CAS)
	// - getting and updating registration cache
	gc.registrationMu.Lock()

	slotsSinceLastRegistration := currentSlot - gc.registrationLastSlot
	operatorSubmissionSlotModulo := gc.operatorID % slotsPerEpoch

	hasRegistrations := len(gc.registrationCache) != 0
	operatorSubmissionSlot := uint64(currentSlot)%slotsPerEpoch == operatorSubmissionSlotModulo
	oneEpochPassed := slotsSinceLastRegistration >= phase0.Slot(slotsPerEpoch)
	twoEpochsAndOperatorDelayPassed := uint64(slotsSinceLastRegistration) >= slotsPerEpoch*2+operatorSubmissionSlotModulo

	if hasRegistrations && (oneEpochPassed && operatorSubmissionSlot || twoEpochsAndOperatorDelayPassed) {
		gc.registrationLastSlot = currentSlot
		registrations := gc.registrationList()

		// Release lock after building a registrations list for submission.
		gc.registrationMu.Unlock()

		if err := gc.submitBatchedRegistrations(currentSlot, registrations); err != nil {
			gc.log.Error("Failed to submit validator registrations",
				zap.Error(err),
				fields.Slot(currentSlot))
		}

		return
	}

	gc.registrationMu.Unlock()
}

// registrationList is not thread-safe
func (gc *goClient) registrationList() []*api.VersionedSignedValidatorRegistration {
	result := make([]*api.VersionedSignedValidatorRegistration, 0)

	for _, registration := range gc.registrationCache {
		result = append(result, registration)
	}

	return result
}

func (gc *goClient) submitBatchedRegistrations(slot phase0.Slot, registrations []*api.VersionedSignedValidatorRegistration) error {
	gc.log.Info("going to submit batch validator registrations",
		fields.Slot(slot),
		fields.Count(len(registrations)))

	for len(registrations) != 0 {
		bs := batchSize
		if bs > len(registrations) {
			bs = len(registrations)
		}

		if err := gc.client.SubmitValidatorRegistrations(gc.ctx, registrations[0:bs]); err != nil {
			return err
		}

		registrations = registrations[bs:]

		gc.log.Info("submitted batched validator registrations",
			fields.Slot(slot),
			fields.Count(bs))
	}

	return nil
}
