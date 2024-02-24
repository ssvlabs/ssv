package goclient

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/operator/slotticker"
)

const (
	batchSize = 500

	// parallelFetchBlindedBlockPatience is the time to wait for blinded block before falling back to full block.
	parallelFetchBlindedBlockPatience = 2500 * time.Millisecond
)

// ProposerDuties returns proposer duties for the given epoch.
func (gc *goClient) ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	resp, err := gc.client.ProposerDuties(ctx, &api.ProposerDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain proposer duties: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("proposer duties response is nil")
	}

	return resp.Data, nil
}

// GetBeaconBlock returns beacon block by the given slot, graffiti, and randao.
func (gc *goClient) GetBeaconBlock(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	reqStart := time.Now()
	proposalResp, err := gc.client.Proposal(gc.ctx, &api.ProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get proposal: %w", err)
	}
	if proposalResp == nil {
		return nil, DataVersionNil, fmt.Errorf("proposal response is nil")
	}
	if proposalResp.Data == nil {
		return nil, DataVersionNil, fmt.Errorf("proposal data is nil")
	}

	metricsProposerDataRequest.Observe(time.Since(reqStart).Seconds())
	beaconBlock := proposalResp.Data

	switch beaconBlock.Version {
	case spec.DataVersionCapella:
		if beaconBlock.Capella == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block is nil")
		}
		if beaconBlock.Capella.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}
		if beaconBlock.Capella.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload is nil")
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	case spec.DataVersionDeneb:
		if beaconBlock.Deneb == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block contents is nil")
		}
		if beaconBlock.Deneb.Block == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block is nil")
		}
		if beaconBlock.Deneb.Block.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block body is nil")
		}
		if beaconBlock.Deneb.Block.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block execution payload is nil")
		}
		return beaconBlock.Deneb, beaconBlock.Version, nil

	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

func (gc *goClient) GetBlindedBeaconBlock(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	if gc.nodeClient == NodePrysm {
		return gc.V3Proposal(slot, graffitiBytes, randao)
	}
	return gc.DefaultGetBlindedBeaconBlock(slot, graffitiBytes, randao)
}

// GetParallelBlocks fetches blinded and non-blinded blocks in parallel to save time. if blinded fails, non blinded is ready without waiting.
func (gc *goClient) GetParallelBlocks(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	type blockFetchResult struct {
		obj ssz.Marshaler
		ver spec.DataVersion
		err error
	}
	blindedBlockCh := make(chan blockFetchResult, 1)
	fullBlockCh := make(chan blockFetchResult, 1)

	// Fetch blinded and non-blinded blocks in parallel.
	blindedBlockTimeout := time.After(parallelFetchBlindedBlockPatience)
	go func() {
		o, v, e := gc.DefaultGetBlindedBeaconBlock(slot, graffitiBytes, randao)
		blindedBlockCh <- blockFetchResult{o, v, e}
	}()
	go func() {
		gc.log.Debug("GetParallelBlocks: GetBeaconBlock starting")
		o, v, e := gc.GetBeaconBlock(slot, graffitiBytes, randao)
		gc.log.Debug("GetParallelBlocks: GetBeaconBlock done", zap.Error(e))
		fullBlockCh <- blockFetchResult{o, v, e}
		gc.log.Debug("GetParallelBlocks: GetBeaconBlock sent", zap.Error(e))
	}()

	// Wait for blinded block with a timeout.
	var result blockFetchResult
	select {
	case result = <-blindedBlockCh:
	case <-blindedBlockTimeout:
		result.err = errors.New("timed out waiting for blinded block")
	}

	// If blinded block failed, wait for full block and return it.
	if result.err != nil {
		gc.log.Debug("🧊 failed to get blinded block, falling back to full block", zap.Error(result.err))
		result = <-fullBlockCh
		gc.log.Debug("GetParallelBlocks: GetBeaconBlock received", zap.Error(result.err))
		if result.err != nil {
			return nil, result.ver, fmt.Errorf("failed to get full block: %w", result.err)
		}
	}

	return result.obj, result.ver, nil
}

// GetBlindedBeaconBlock returns blinded beacon block by the given slot, graffiti, and randao.
func (gc *goClient) DefaultGetBlindedBeaconBlock(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {

	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	reqStart := time.Now()
	blindedProposalResp, err := gc.client.BlindedProposal(gc.ctx, &api.BlindedProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get blinded proposal: %w", err)
	}
	if blindedProposalResp == nil {
		return nil, DataVersionNil, fmt.Errorf("blinded proposal response is nil")
	}
	if blindedProposalResp.Data == nil {
		return nil, DataVersionNil, fmt.Errorf("blinded proposal data is nil")
	}

	metricsProposerDataRequest.Observe(time.Since(reqStart).Seconds())
	beaconBlock := blindedProposalResp.Data

	switch beaconBlock.Version {
	case spec.DataVersionCapella:
		if beaconBlock.Capella == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block is nil")
		}
		if beaconBlock.Capella.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}
		if beaconBlock.Capella.Body.ExecutionPayloadHeader == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload header is nil")
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	case spec.DataVersionDeneb:
		if beaconBlock.Deneb == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block contents is nil")
		}
		if beaconBlock.Deneb.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block body is nil")
		}
		if beaconBlock.Deneb.Body.ExecutionPayloadHeader == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block execution payload header is nil")
		}
		return beaconBlock.Deneb, beaconBlock.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

func (gc *goClient) V3Proposal(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	reqStart := time.Now()
	v3ProposalResp, err := gc.client.V3Proposal(gc.ctx, &api.V3ProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get blinded proposal: %w", err)
	}
	if v3ProposalResp == nil {
		return nil, DataVersionNil, fmt.Errorf("blinded proposal response is nil")
	}
	if v3ProposalResp.Data == nil {
		return nil, DataVersionNil, fmt.Errorf("blinded proposal data is nil")
	}

	metricsProposerDataRequest.Observe(time.Since(reqStart).Seconds())
	beaconBlock := v3ProposalResp.Data

	if beaconBlock.ExecutionPayloadBlinded {
		switch beaconBlock.Version {
		case spec.DataVersionCapella:
			if beaconBlock.BlindedCapella == nil {
				return nil, DataVersionNil, fmt.Errorf("capella block is nil")
			}
			if beaconBlock.BlindedCapella.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
			}
			if beaconBlock.BlindedCapella.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("capella block execution payload header is nil")
			}
			return beaconBlock.BlindedCapella, beaconBlock.Version, nil
		case spec.DataVersionDeneb:
			if beaconBlock.BlindedDeneb == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb block contents is nil")
			}
			if beaconBlock.BlindedDeneb.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb block body is nil")
			}
			if beaconBlock.BlindedDeneb.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb block execution payload header is nil")
			}
			return beaconBlock.BlindedDeneb, beaconBlock.Version, nil
		default:
			return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
		}
	}

	switch beaconBlock.Version {
	case spec.DataVersionCapella:
		if beaconBlock.Capella == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block is nil")
		}
		if beaconBlock.Capella.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block body is nil")
		}
		if beaconBlock.Capella.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("capella block execution payload is nil")
		}
		return beaconBlock.Capella, beaconBlock.Version, nil
	case spec.DataVersionDeneb:
		if beaconBlock.Deneb == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block contents is nil")
		}
		if beaconBlock.Deneb.Block == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block is nil")
		}
		if beaconBlock.Deneb.Block.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block body is nil")
		}
		if beaconBlock.Deneb.Block.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb block execution payload is nil")
		}
		return beaconBlock.Deneb, beaconBlock.Version, nil

	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}

}

func (gc *goClient) SubmitBlindedBeaconBlock(block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error {
	signedBlock := &api.VersionedSignedBlindedProposal{
		Version: block.Version,
	}
	switch block.Version {
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return fmt.Errorf("capella blinded block is nil")
		}
		signedBlock.Capella = &apiv1capella.SignedBlindedBeaconBlock{
			Message: block.Capella,
		}
		copy(signedBlock.Capella.Signature[:], sig[:])
	case spec.DataVersionDeneb:
		if block.Deneb == nil {
			return fmt.Errorf("deneb block contents is nil")
		}
		if block.Deneb.Body == nil {
			return fmt.Errorf("deneb block body is nil")
		}
		if block.Deneb.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("deneb block execution payload header is nil")
		}
		signedBlock.Deneb = &apiv1deneb.SignedBlindedBeaconBlock{
			Message: block.Deneb,
		}
		copy(signedBlock.Deneb.Signature[:], sig[:])
	default:
		return fmt.Errorf("unknown block version")
	}

	return gc.client.SubmitBlindedProposal(gc.ctx, signedBlock)
}

// SubmitBeaconBlock submit the block to the node
func (gc *goClient) SubmitBeaconBlock(block *api.VersionedProposal, sig phase0.BLSSignature) error {
	signedBlock := &api.VersionedSignedProposal{
		Version: block.Version,
	}
	switch block.Version {
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return fmt.Errorf("capella block is nil")
		}
		signedBlock.Capella = &capella.SignedBeaconBlock{
			Message: block.Capella,
		}
		copy(signedBlock.Capella.Signature[:], sig[:])
	case spec.DataVersionDeneb:
		if block.Deneb == nil {
			return fmt.Errorf("deneb block contents is nil")
		}
		if block.Deneb.Block == nil {
			return fmt.Errorf("deneb block is nil")
		}
		if block.Deneb.Block.Body == nil {
			return fmt.Errorf("deneb block body is nil")
		}
		if block.Deneb.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("deneb block execution payload header is nil")
		}
		signedBlock.Deneb = &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message: block.Deneb.Block,
			},
			KZGProofs: block.Deneb.KZGProofs,
			Blobs:     block.Deneb.Blobs,
		}
		copy(signedBlock.Deneb.SignedBlock.Signature[:], sig[:])
	default:
		return fmt.Errorf("unknown block version")
	}

	return gc.client.SubmitProposal(gc.ctx, signedBlock)
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
