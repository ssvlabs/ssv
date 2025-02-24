package goclient

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/slotticker"
)

const (
	batchSize = 500
)

// ProposerDuties returns proposer duties for the given epoch.
func (gc *GoClient) ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	start := time.Now()
	resp, err := gc.multiClient.ProposerDuties(ctx, &api.ProposerDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequestDuration(gc.ctx, "ProposerDuties", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)

	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "ProposerDuties"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain proposer duties: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "ProposerDuties"),
		)
		return nil, fmt.Errorf("proposer duties response is nil")
	}

	return resp.Data, nil
}

// GetBeaconBlock returns beacon block by the given slot, graffiti, and randao.
func (gc *GoClient) GetBeaconBlock(slot phase0.Slot, graffitiBytes, randao []byte) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	reqStart := time.Now()
	proposalResp, err := gc.multiClient.Proposal(gc.ctx, &api.ProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	recordRequestDuration(gc.ctx, "Proposal", gc.multiClient.Address(), http.MethodGet, time.Since(reqStart), err)

	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Proposal"),
			zap.Error(err),
		)
		return nil, DataVersionNil, fmt.Errorf("failed to get proposal: %w", err)
	}
	if proposalResp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "Proposal"),
		)
		return nil, DataVersionNil, fmt.Errorf("proposal response is nil")
	}
	if proposalResp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "Proposal"),
		)
		return nil, DataVersionNil, fmt.Errorf("proposal data is nil")
	}

	beaconBlock := proposalResp.Data

	if beaconBlock.Blinded {
		switch beaconBlock.Version {
		case spec.DataVersionCapella:
			if beaconBlock.CapellaBlinded == nil {
				return nil, DataVersionNil, fmt.Errorf("capella blinded block is nil")
			}
			if beaconBlock.CapellaBlinded.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("capella blinded block body is nil")
			}
			if beaconBlock.CapellaBlinded.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("capella blinded block execution payload header is nil")
			}
			return beaconBlock.CapellaBlinded, beaconBlock.Version, nil
		case spec.DataVersionDeneb:
			if beaconBlock.DenebBlinded == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb blinded block contents is nil")
			}
			if beaconBlock.DenebBlinded.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb blinded block body is nil")
			}
			if beaconBlock.DenebBlinded.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("deneb blinded block execution payload header is nil")
			}
			return beaconBlock.DenebBlinded, beaconBlock.Version, nil
		case spec.DataVersionElectra:
			if beaconBlock.ElectraBlinded == nil {
				return nil, DataVersionNil, fmt.Errorf("electra blinded block is nil")
			}
			if beaconBlock.ElectraBlinded.Body == nil {
				return nil, DataVersionNil, fmt.Errorf("electra blinded block body is nil")
			}
			if beaconBlock.ElectraBlinded.Body.ExecutionPayloadHeader == nil {
				return nil, DataVersionNil, fmt.Errorf("electra blinded block execution payload header is nil")
			}
			return beaconBlock.ElectraBlinded, beaconBlock.Version, nil
		default:
			return nil, DataVersionNil, fmt.Errorf("beacon blinded block version %s not supported", beaconBlock.Version)
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
	case spec.DataVersionElectra:
		if beaconBlock.Electra == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block contents is nil")
		}
		if beaconBlock.Electra.Block == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block is nil")
		}
		if beaconBlock.Electra.Block.Body == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block body is nil")
		}
		if beaconBlock.Electra.Block.Body.ExecutionPayload == nil {
			return nil, DataVersionNil, fmt.Errorf("electra block execution payload is nil")
		}
		return beaconBlock.Electra, beaconBlock.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("beacon block version %s not supported", beaconBlock.Version)
	}
}

func (gc *GoClient) SubmitBlindedBeaconBlock(block *api.VersionedBlindedProposal, sig phase0.BLSSignature) error {
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
	case spec.DataVersionElectra:
		if block.Electra == nil {
			return fmt.Errorf("electra block contents is nil")
		}
		if block.Electra.Body == nil {
			return fmt.Errorf("electra block body is nil")
		}
		if block.Electra.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("electra block execution payload header is nil")
		}
		signedBlock.Electra = &apiv1electra.SignedBlindedBeaconBlock{
			Message: block.Electra,
		}
		copy(signedBlock.Electra.Signature[:], sig[:])
	default:
		return fmt.Errorf("unknown block version")
	}

	opts := &api.SubmitBlindedProposalOpts{
		Proposal: signedBlock,
	}

	// As gc.multiClient doesn't have a method for blinded block submission
	// (because it must be submitted to the same node that returned that block),
	// we need to submit it to client(s) directly.
	if len(gc.clients) == 1 {
		clientAddress := gc.clients[0].Address()
		logger := gc.log.With(
			zap.String("api", "SubmitBlindedProposal"),
			zap.String("client_addr", clientAddress))

		start := time.Now()
		err := gc.clients[0].SubmitBlindedProposal(gc.ctx, opts)
		recordRequestDuration(gc.ctx, "SubmitBlindedProposal", clientAddress, http.MethodPost, time.Since(start), err)
		if err != nil {
			logger.Error(clResponseErrMsg,
				zap.Error(err),
			)
			return err
		}

		logger.Debug("consensus client submitted blinded beacon block")

		return nil
	}

	// Although we got a blinded block from one node and that node has to submit it,
	// other nodes might know this payload too and have a chance to submit it successfully.
	//
	// So we do the following:
	//
	// Submit the blinded proposal to all clients concurrently.
	// If any client succeeds, cancel the remaining submissions.
	// Wait for all submissions to finish or timeout after 1 minute.
	//
	// TODO: Make sure this the above is correct. Should we submit only to the node that returned the block?

	logger := gc.log.With(zap.String("api", "SubmitBlindedProposal"))

	submissions := atomic.Int32{}
	p := pool.New().WithErrors().WithContext(gc.ctx)
	for _, client := range gc.clients {
		client := client
		p.Go(func(ctx context.Context) error {
			clientAddress := client.Address()
			logger := logger.With(zap.String("client_addr", clientAddress))

			if err := client.SubmitBlindedProposal(ctx, opts); err == nil {
				logger.Debug("consensus client returned an error while submitting blinded proposal. As at least one node must submit successfully, it's expected that some nodes may fail to submit.",
					zap.Error(err))
				return err
			}

			logger.Debug("consensus client submitted blinded beacon block")

			submissions.Add(1)
			return nil
		})
	}
	err := p.Wait()
	if submissions.Load() > 0 {
		// At least one client has submitted the proposal successfully,
		// so we can return without error.
		return nil
	}
	if err != nil {
		logger.Error("no consensus clients have been able to submit blinded proposal. See adjacent logs for error details.")
		return fmt.Errorf("no consensus clients have been able to submit blinded proposal")
	}
	return nil
}

// SubmitBeaconBlock submit the block to the node
func (gc *GoClient) SubmitBeaconBlock(block *api.VersionedProposal, sig phase0.BLSSignature) error {
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
	case spec.DataVersionElectra:
		if block.Electra == nil {
			return fmt.Errorf("electra block contents is nil")
		}
		if block.Electra.Block == nil {
			return fmt.Errorf("electra block is nil")
		}
		if block.Electra.Block.Body == nil {
			return fmt.Errorf("electra block body is nil")
		}
		if block.Electra.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("electra block execution payload header is nil")
		}
		signedBlock.Electra = &apiv1electra.SignedBlockContents{
			SignedBlock: &electra.SignedBeaconBlock{
				Message: block.Electra.Block,
			},
			KZGProofs: block.Electra.KZGProofs,
			Blobs:     block.Electra.Blobs,
		}
		copy(signedBlock.Electra.SignedBlock.Signature[:], sig[:])
	default:
		return fmt.Errorf("unknown block version")
	}

	opts := &api.SubmitProposalOpts{
		Proposal: signedBlock,
	}

	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitProposal"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitProposal(gc.ctx, opts)
	recordRequestDuration(gc.ctx, "SubmitProposal", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted beacon block")
	return nil
}

func (gc *GoClient) SubmitValidatorRegistration(registration *api.VersionedSignedValidatorRegistration) error {
	return gc.updateBatchRegistrationCache(registration)
}

func (gc *GoClient) SubmitProposalPreparation(feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress) error {
	var preparations []*eth2apiv1.ProposalPreparation
	for index, recipient := range feeRecipients {
		preparations = append(preparations, &eth2apiv1.ProposalPreparation{
			ValidatorIndex: index,
			FeeRecipient:   recipient,
		})
	}

	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitProposalPreparations"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitProposalPreparations(gc.ctx, preparations)
	recordRequestDuration(gc.ctx, "SubmitProposalPreparations", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted proposal preparation")
	return nil
}

func (gc *GoClient) updateBatchRegistrationCache(registration *api.VersionedSignedValidatorRegistration) error {
	pk, err := registration.PubKey()
	if err != nil {
		return err
	}

	gc.registrationMu.Lock()
	defer gc.registrationMu.Unlock()

	gc.registrationCache[pk] = registration
	return nil
}

func (gc *GoClient) registrationSubmitter(slotTickerProvider slotticker.Provider) {
	operatorID := gc.operatorDataStore.AwaitOperatorID()

	ticker := slotTickerProvider()
	for {
		select {
		case <-gc.ctx.Done():
			return
		case <-ticker.Next():
			gc.submitRegistrationsFromCache(ticker.Slot(), operatorID)
		}
	}
}

func (gc *GoClient) submitRegistrationsFromCache(currentSlot phase0.Slot, operatorID spectypes.OperatorID) {
	slotsPerEpoch := gc.network.SlotsPerEpoch()

	// Lock:
	// - getting and updating last slot to avoid multiple submission (both should be an atomic action but cannot be done with CAS)
	// - getting and updating registration cache
	gc.registrationMu.Lock()

	slotsSinceLastRegistration := currentSlot - gc.registrationLastSlot
	operatorSubmissionSlotModulo := operatorID % slotsPerEpoch

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
func (gc *GoClient) registrationList() []*api.VersionedSignedValidatorRegistration {
	result := make([]*api.VersionedSignedValidatorRegistration, 0)

	for _, registration := range gc.registrationCache {
		result = append(result, registration)
	}

	return result
}

func (gc *GoClient) submitBatchedRegistrations(slot phase0.Slot, registrations []*api.VersionedSignedValidatorRegistration) error {
	gc.log.Info("going to submit batch validator registrations",
		fields.Slot(slot),
		fields.Count(len(registrations)))

	for len(registrations) != 0 {
		bs := batchSize
		if bs > len(registrations) {
			bs = len(registrations)
		}

		clientAddress := gc.multiClient.Address()
		logger := gc.log.With(
			zap.String("api", "SubmitValidatorRegistrations"),
			zap.String("client_addr", clientAddress))

		// TODO: Do we need to submit them to all nodes?
		start := time.Now()
		err := gc.multiClient.SubmitValidatorRegistrations(gc.ctx, registrations[0:bs])
		recordRequestDuration(gc.ctx, "SubmitValidatorRegistrations", clientAddress, http.MethodPost, time.Since(start), err)
		if err != nil {
			logger.Error(clResponseErrMsg, zap.Error(err))
			return err
		}

		registrations = registrations[bs:]

		logger.Info("submitted batched validator registrations",
			fields.Slot(slot),
			fields.Count(bs))
	}

	return nil
}
