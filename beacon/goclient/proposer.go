package goclient

import (
	"context"
	"fmt"
	"net/http"
	"sync"
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
	"github.com/ssvlabs/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
)

// ProposerDuties returns proposer duties for the given epoch.
func (gc *GoClient) ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	start := time.Now()
	resp, err := gc.multiClient.ProposerDuties(ctx, &api.ProposerDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequestDuration(ctx, "ProposerDuties", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)

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
func (gc *GoClient) GetBeaconBlock(
	ctx context.Context,
	slot phase0.Slot,
	graffitiBytes []byte,
	randao []byte,
) (ssz.Marshaler, spec.DataVersion, error) {
	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	beaconBlock, err := gc.getProposal(ctx, slot, sig, graffiti)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Proposal"),
			zap.Error(err),
		)
		return nil, DataVersionNil, fmt.Errorf("failed to get proposal: %w", err)
	}
	if beaconBlock == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "Proposal"),
		)
		return nil, DataVersionNil, fmt.Errorf("proposal response is nil")
	}

	return extractBlock(beaconBlock)
}

func (gc *GoClient) SubmitBlindedBeaconBlock(
	ctx context.Context,
	block *api.VersionedBlindedProposal,
	sig phase0.BLSSignature,
) error {
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

	return gc.multiClientSubmit(ctx, "SubmitBlindedProposal", func(ctx context.Context, client Client) error {
		return client.SubmitBlindedProposal(ctx, opts)
	})
}

// SubmitBeaconBlock submit the block to the node
func (gc *GoClient) SubmitBeaconBlock(
	ctx context.Context,
	block *api.VersionedProposal,
	sig phase0.BLSSignature,
) error {
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

	return gc.multiClientSubmit(ctx, "SubmitProposal", func(ctx context.Context, client Client) error {
		return client.SubmitProposal(ctx, opts)
	})
}

func (gc *GoClient) SubmitProposalPreparation(
	ctx context.Context,
	feeRecipients map[phase0.ValidatorIndex]bellatrix.ExecutionAddress,
) error {
	var preparations []*eth2apiv1.ProposalPreparation
	for index, recipient := range feeRecipients {
		preparations = append(preparations, &eth2apiv1.ProposalPreparation{
			ValidatorIndex: index,
			FeeRecipient:   recipient,
		})
	}

	gc.submitProposalPreparationBatches(ctx, preparations)
	gc.feeRecipientsCache.Store(&feeRecipients)
	return nil
}

func (gc *GoClient) submitProposalPreparationBatches(
	ctx context.Context,
	preparations []*eth2apiv1.ProposalPreparation,
) {
	const batchSize = 500
	submitted := 0
	for start := 0; start < len(preparations); start += batchSize {
		end := start + batchSize
		if end > len(preparations) {
			end = len(preparations)
		}
		batch := preparations[start:end]

		err := gc.multiClientSubmit(ctx, "SubmitProposalPreparations", func(ctx context.Context, client Client) error {
			return client.SubmitProposalPreparations(ctx, batch)
		})
		if err != nil {
			gc.log.Error("could not submit proposal preparation batch",
				zap.Int("start_index", start),
				zap.Error(err),
			)
			continue
		}
		submitted += len(batch)
	}

	switch {
	case submitted == len(preparations):
		gc.log.Debug("✅ successfully submitted all proposal preparations",
			zap.Int("total", len(preparations)),
		)

	case submitted > 0:
		gc.log.Warn("⚠️ partially submitted proposal preparations",
			zap.Int("submitted", submitted),
			zap.Int("errored", len(preparations)-submitted),
			zap.Int("total", len(preparations)),
		)

	default:
		gc.log.Error("❗️couldn't submit any proposal preparations",
			zap.Int("total", len(preparations)),
		)
	}
}

// getProposal fetches proposals from beacon nodes and
// returns the first received one with a fee recipient set,
// or, if none, it returns the first received proposal.
//
// If it receives no proposal until context is canceled,
// it returns an error.
//
// If using one client, it just returns the fetched proposal
// if it has a fee recipient. Otherwise, it tries to submit
// a proposal preparation for this validator and request
// a new block with a fee recipient using a strict deadline.
func (gc *GoClient) getProposal(
	ctx context.Context,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	if len(gc.clients) == 1 {
		return gc.getSingleClientProposal(ctx, slot, sig, graffiti)
	}

	return gc.getMultiClientProposal(ctx, slot, sig, graffiti)
}

func (gc *GoClient) getSingleClientProposal(
	ctx context.Context,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	logger := gc.log.With(fields.Address(gc.clients[0].Address()))

	reqStart := time.Now()
	p, err := gc.clients[0].Proposal(ctx, &api.ProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	recordRequestDuration(ctx, "Proposal", gc.clients[0].Address(), http.MethodGet, time.Since(reqStart), err)
	if err != nil {
		return nil, err
	}

	ok, err := hasFeeRecipient(p.Data)
	if err != nil {
		// If we cannot check fee recipient, either the block is malformed
		// or hasFeeRecipient doesn't count all cases.
		// So we just log it, return the block we received,
		// and let the caller decide what to do with it.
		logger.Error("failed to check fee recipient", zap.Error(err))
		return p.Data, nil
	}
	if ok {
		return p.Data, nil
	}

	// Although proposal preparations are submitted on beacon node restart,
	// the beacon node may return no fee recipient because we submit
	// proposal preparations in batches and don't interrupt the process on error.
	//
	// So, given the load on the node on restart, it's possible
	// that registrations haven't been submitted, although it's unlikely.
	//
	// Therefore, we may have to try to submit them again
	// and request another block using a strict deadline that will, hopefully,
	// have a fee recipient set. The deadline aims to have enough time
	// to complete a QBFT consensus within the first two rounds.
	//
	// Trying to do it within the first round might also work.
	// But in the case with the single client (unlike the multi client),
	// we have to submit proposal preparations and request a new proposal.
	// So, it requires more time, and the first round becomes very risky.
	feeRecipientDeadline := gc.latestProposalTime(slot, 2)
	feeRecipientCtx, feeRecipientCancel := context.WithDeadline(ctx, feeRecipientDeadline)
	defer feeRecipientCancel()

	logger.Warn("received a proposal without fee recipients, trying to submit a preparation and get a new one",
		zap.Duration("time_left", time.Until(feeRecipientDeadline)))

	validatorIndex, err := getValidatorIndex(p.Data)
	if err != nil {
		logger.Error("failed to extract validator index from proposal", zap.Error(err))
		return p.Data, nil
	}

	logger = logger.With(fields.ValidatorIndex(validatorIndex))

	newProposalStart := time.Now()
	feeRecipientProposal, err := gc.submitPreparationsAndGetProposal(feeRecipientCtx, validatorIndex, slot, sig, graffiti)
	if err != nil {
		logger.Error("failed to submit a preparation and get a new proposal",
			fields.Took(time.Since(newProposalStart)),
			zap.Error(err))
		return p.Data, nil
	}

	logger.Info("received a new proposal with fee recipient",
		fields.Took(time.Since(newProposalStart)),
		fields.Address(mustGetFeeRecipient(feeRecipientProposal).String()))

	return feeRecipientProposal, nil
}

// submitPreparationsAndGetProposal submits a proposal preparation from cache for given validator index
// and requests a proposal from CL afterward. This logic is relevant only for a single client.
func (gc *GoClient) submitPreparationsAndGetProposal(
	ctx context.Context,
	validatorIndex phase0.ValidatorIndex,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	feeRecipients := gc.feeRecipientsCache.Load()
	if feeRecipients == nil {
		// The cache shouldn't be empty because proposal preparations are submitted on the node start,
		// but it's better to check it to avoid nil pointer dereference.
		return nil, fmt.Errorf("received a proposal without fee recipients but the fee recipients data hasn't been built yet")
	}

	address, ok := (*feeRecipients)[validatorIndex]
	if !ok {
		return nil, fmt.Errorf("fee recipient address for validator not found")
	}

	proposalPreparation := []*eth2apiv1.ProposalPreparation{
		{
			ValidatorIndex: validatorIndex,
			FeeRecipient:   address,
		},
	}

	preparationsReqStart := time.Now()
	err := gc.clients[0].SubmitProposalPreparations(ctx, proposalPreparation)
	recordRequestDuration(ctx, "SubmitProposalPreparations", gc.clients[0].Address(), http.MethodGet, time.Since(preparationsReqStart), err)
	if err != nil {
		return nil, fmt.Errorf("submit proposal preparation: %w", err)
	}

	proposalReqStart := time.Now()
	newProposal, err := gc.clients[0].Proposal(ctx, &api.ProposalOpts{
		Slot:                   slot,
		RandaoReveal:           sig,
		Graffiti:               graffiti,
		SkipRandaoVerification: false,
	})
	recordRequestDuration(ctx, "Proposal", gc.clients[0].Address(), http.MethodGet, time.Since(proposalReqStart), err)
	if err != nil {
		return nil, fmt.Errorf("get proposal: %w", err)
	}

	ok, err = hasFeeRecipient(newProposal.Data)
	if err != nil {
		return nil, fmt.Errorf("extract proposal fee recipient: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("received a proposal after preparation submission but it still doesn't have a fee recipient")
	}

	return newProposal.Data, nil
}

func (gc *GoClient) getMultiClientProposal(
	ctx context.Context,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	// Cancel the goroutines when we got a result to avoid waiting for the parent context.
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()

	proposalsWithFeeRecipient, proposals := gc.getProposalStreams(requestCtx, slot, sig, graffiti)

	return gc.selectBestProposal(ctx, slot, proposalsWithFeeRecipient, proposals)
}

func (gc *GoClient) getProposalStreams(
	ctx context.Context,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (chan *api.VersionedProposal, chan *api.VersionedProposal) {
	// Although we re-submit proposal preparation on client restart,
	// the spec doesn't guarantee it will be used:
	//
	// https://ethereum.github.io/beacon-APIs/#/Validator/prepareBeaconProposer
	// > Note that there is no guarantee that the beacon node will
	// > use the supplied fee recipient when creating a block proposal,
	// > so on receipt of a proposed block the validator should confirm
	// > that it finds the fee recipient within the block acceptable before signing it.
	//
	// So we nevertheless prioritize finding a proposal with a fee recipient.
	proposalsWithFeeRecipient := make(chan *api.VersionedProposal, len(gc.clients))
	proposals := make(chan *api.VersionedProposal, len(gc.clients))

	var wg sync.WaitGroup

	for _, client := range gc.clients {
		wg.Add(1)
		go func() {
			defer wg.Done()

			reqStart := time.Now()
			proposalResp, err := client.Proposal(ctx, &api.ProposalOpts{
				Slot:                   slot,
				RandaoReveal:           sig,
				Graffiti:               graffiti,
				SkipRandaoVerification: false,
			})
			recordRequestDuration(ctx, "Proposal", client.Address(), http.MethodGet, time.Since(reqStart), err)
			if err != nil {
				gc.log.Warn("failed to get proposal", fields.Address(client.Address()), zap.Error(err))
				return
			}

			ok, err := hasFeeRecipient(proposalResp.Data)
			if err != nil {
				// If we cannot check fee recipient, either the block is malformed
				// or hasFeeRecipient doesn't count all cases.
				// So we just log it, consider the block without fee recipient,
				// and let the caller decide what to do with it.
				gc.log.Error("failed to check fee recipient", fields.Address(client.Address()), zap.Error(err))
			}
			if ok {
				proposalsWithFeeRecipient <- proposalResp.Data
			}

			proposals <- proposalResp.Data
		}()
	}

	go func() {
		wg.Wait()
		close(proposals)
		close(proposalsWithFeeRecipient)
	}()

	return proposalsWithFeeRecipient, proposals
}

func (gc *GoClient) selectBestProposal(
	ctx context.Context,
	slot phase0.Slot,
	proposalsWithFeeRecipient chan *api.VersionedProposal,
	proposals chan *api.VersionedProposal,
) (*api.VersionedProposal, error) {
	// Try to get a proposal with a fee recipient set before feeRecipientDeadline
	// while we are within the first slot.
	// Afterward, any proposal will be enough for us.
	// The second round might also work, giving more time to get
	// a proposal we are looking for, but it would increase the probability
	// of being late with the proposal submission.
	feeRecipientCtx, feeRecipientCancel := context.WithDeadline(ctx, gc.latestProposalTime(slot, 1))
	defer feeRecipientCancel()

	var selectProposalCh chan *api.VersionedProposal

	// Wrapped with a for loop to enter the select again on feeRecipientCtx expiration.
	for {
		select {
		case p, ok := <-proposalsWithFeeRecipient:
			if !ok {
				// All request goroutines have finished, but we didn't get any proposal with fee recipient.

				// proposalsWithFeeRecipient is closed after proposals, so this should never block.
				p, ok = <-proposals
				if !ok {
					// We don't have any proposals, so we got some error.
					// The errors have been logged, so we don't need to return them.
					return nil, fmt.Errorf("all requests failed")
				}

				// If we have one without fee recipient, it's better that nothing, so we use it.
				gc.log.Warn("no proposals with fee recipient found")
				return p, nil

			}
			// Got a proposal with fee recipient. It is good enough, we can use it.
			gc.log.Debug("received a proposal with fee recipient")
			return p, nil
		case p, ok := <-selectProposalCh:
			// Didn't get a proposal with fee recipient until feeRecipientCtx expired,
			// so we are ready to accept any proposal.
			if !ok {
				// We don't have any proposals, so we got some error.
				// The errors have been logged, so we don't need to return them.
				return nil, fmt.Errorf("all requests failed")
			}

			gc.log.Warn("no proposals with fee recipient found")
			return p, nil
		case <-feeRecipientCtx.Done():
			// Didn't get a proposal with fee recipient quickly,
			// enable receiving from proposals and enter the select again.
			selectProposalCh = proposals
			gc.log.Debug("didn't receive proposal with fee recipient within the safe duration for the best round, accepting any proposal")
		case <-ctx.Done():
			// Ran out of time. Check if we got any proposal.
			select {
			case p, ok := <-proposals:
				if !ok {
					// Ran out of time and then all requests immediately finished.
					// This should probably never happen, but it's better to make sure we won't return a nil proposal.
					return nil, ctx.Err()
				}

				// If we have one without fee recipient, it's better that nothing, so we use it.
				gc.log.Warn("no proposals with fee recipient found")
				return p, nil
			default:
				// We still don't have any proposals, but requests haven't finished yet.
				// However, we don't have more time, so we have to return an error.
				// Probably, there are some connection issues or the node is overloaded.
				return nil, ctx.Err()
			}
		}
	}
}

// latestProposalTime returns the latest time when QBFT should start
// to have a high chance to finish within the best time range for given slot.
func (gc *GoClient) latestProposalTime(slot phase0.Slot, maxRound qbft.Round) time.Time {
	const avgQBFTDuration = 350 * time.Millisecond
	return gc.beaconConfig.GetSlotStartTime(slot).Add(time.Duration(maxRound)*qbft.QuickTimeout - avgQBFTDuration*2)
}

func hasFeeRecipient(block any) (bool, error) {
	var zeroAddress bellatrix.ExecutionAddress

	address, err := getFeeRecipient(block)
	if err != nil {
		return false, err
	}

	return address != zeroAddress, nil
}

func mustGetFeeRecipient(block any) bellatrix.ExecutionAddress {
	b, err := getFeeRecipient(block)
	if err != nil {
		panic(err)
	}
	return b
}

func getFeeRecipient(block any) (bellatrix.ExecutionAddress, error) {
	if block == nil {
		return bellatrix.ExecutionAddress{}, fmt.Errorf("block is nil")
	}
	switch b := block.(type) {
	case *api.VersionedProposal:
		if b.Blinded {
			switch b.Version {
			case spec.DataVersionCapella:
				return getFeeRecipient(b.CapellaBlinded)
			case spec.DataVersionDeneb:
				return getFeeRecipient(b.DenebBlinded)
			case spec.DataVersionElectra:
				return getFeeRecipient(b.ElectraBlinded)
			default:
				return bellatrix.ExecutionAddress{}, fmt.Errorf("unsupported blinded block version %d", b.Version)
			}
		}
		switch b.Version {
		case spec.DataVersionCapella:
			return getFeeRecipient(b.Capella)
		case spec.DataVersionDeneb:
			if b.Deneb == nil {
				return bellatrix.ExecutionAddress{}, fmt.Errorf("deneb block contents is nil")
			}
			return getFeeRecipient(b.Deneb.Block)
		case spec.DataVersionElectra:
			if b.Electra == nil {
				return bellatrix.ExecutionAddress{}, fmt.Errorf("electra block contents is nil")
			}
			return getFeeRecipient(b.Electra.Block)
		default:
			return bellatrix.ExecutionAddress{}, fmt.Errorf("unsupported block version %d", b.Version)
		}

	case *api.VersionedBlindedProposal:
		switch b.Version {
		case spec.DataVersionCapella:
			return getFeeRecipient(b.Capella)
		case spec.DataVersionDeneb:
			return getFeeRecipient(b.Deneb)
		case spec.DataVersionElectra:
			return getFeeRecipient(b.Electra)
		default:
			return bellatrix.ExecutionAddress{}, fmt.Errorf("unsupported blinded block version %d", b.Version)
		}

	case *capella.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("block, body or execution payload is nil")
		}
		return b.Body.ExecutionPayload.FeeRecipient, nil

	case *deneb.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("block, body or execution payload is nil")
		}
		return b.Body.ExecutionPayload.FeeRecipient, nil

	case *electra.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("block, body or execution payload is nil")
		}
		return b.Body.ExecutionPayload.FeeRecipient, nil

	case *apiv1electra.BlockContents:
		return getFeeRecipient(b.Block)

	case *apiv1deneb.BlockContents:
		return getFeeRecipient(b.Block)

	case *apiv1capella.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("block, body or execution payload header is nil")
		}
		return b.Body.ExecutionPayloadHeader.FeeRecipient, nil

	case *apiv1deneb.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("block, body or execution payload header is nil")
		}
		return b.Body.ExecutionPayloadHeader.FeeRecipient, nil

	case *apiv1electra.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("block, body or execution payload header is nil")
		}
		return b.Body.ExecutionPayloadHeader.FeeRecipient, nil
	}

	return bellatrix.ExecutionAddress{}, fmt.Errorf("unsupported block type %T", block)
}

func getValidatorIndex(block any) (phase0.ValidatorIndex, error) {
	if block == nil {
		return 0, fmt.Errorf("block is nil")
	}
	switch b := block.(type) {
	case *api.VersionedProposal:
		if b.Blinded {
			switch b.Version {
			case spec.DataVersionCapella:
				return getValidatorIndex(b.CapellaBlinded)
			case spec.DataVersionDeneb:
				return getValidatorIndex(b.DenebBlinded)
			case spec.DataVersionElectra:
				return getValidatorIndex(b.ElectraBlinded)
			default:
				return 0, fmt.Errorf("unsupported blinded block version %d", b.Version)
			}
		}
		switch b.Version {
		case spec.DataVersionCapella:
			return getValidatorIndex(b.Capella)
		case spec.DataVersionDeneb:
			if b.Deneb == nil {
				return 0, fmt.Errorf("deneb block contents is nil")
			}
			return getValidatorIndex(b.Deneb.Block)
		case spec.DataVersionElectra:
			if b.Electra == nil {
				return 0, fmt.Errorf("electra block contents is nil")
			}
			return getValidatorIndex(b.Electra.Block)
		default:
			return 0, fmt.Errorf("unsupported block version %d", b.Version)
		}

	case *api.VersionedBlindedProposal:
		switch b.Version {
		case spec.DataVersionCapella:
			return getValidatorIndex(b.Capella)
		case spec.DataVersionDeneb:
			return getValidatorIndex(b.Deneb)
		case spec.DataVersionElectra:
			return getValidatorIndex(b.Electra)
		default:
			return 0, fmt.Errorf("unsupported blinded block version %d", b.Version)
		}

	case *capella.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return 0, fmt.Errorf("block, body or execution payload is nil")
		}
		return b.ProposerIndex, nil

	case *deneb.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return 0, fmt.Errorf("block, body or execution payload is nil")
		}
		return b.ProposerIndex, nil

	case *electra.BeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayload == nil {
			return 0, fmt.Errorf("block, body or execution payload is nil")
		}
		return b.ProposerIndex, nil

	case *apiv1electra.BlockContents:
		return getValidatorIndex(b.Block)

	case *apiv1deneb.BlockContents:
		return getValidatorIndex(b.Block)

	case *apiv1capella.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return 0, fmt.Errorf("block, body or execution payload header is nil")
		}
		return b.ProposerIndex, nil

	case *apiv1deneb.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return 0, fmt.Errorf("block, body or execution payload header is nil")
		}
		return b.ProposerIndex, nil

	case *apiv1electra.BlindedBeaconBlock:
		if b == nil || b.Body == nil || b.Body.ExecutionPayloadHeader == nil {
			return 0, fmt.Errorf("block, body or execution payload header is nil")
		}
		return b.ProposerIndex, nil
	}

	return 0, fmt.Errorf("unsupported block type %T", block)
}

func extractBlock(beaconBlock *api.VersionedProposal) (ssz.Marshaler, spec.DataVersion, error) {
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
