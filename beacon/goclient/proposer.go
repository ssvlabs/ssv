package goclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
)

// ProposerDuties returns proposer duties for the given epoch.
func (gc *GoClient) ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error) {
	start := time.Now()
	resp, err := gc.multiClient.ProposerDuties(ctx, &api.ProposerDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequest(ctx, gc.log, "ProposerDuties", gc.multiClient, http.MethodGet, true, time.Since(start), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("fetch proposer duties: %w", err), "ProposerDuties")
	}
	if resp == nil {
		return nil, errMultiClient(fmt.Errorf("proposer duties response is nil"), "ProposerDuties")
	}
	if resp.Data == nil {
		return nil, errMultiClient(fmt.Errorf("proposer duties response data is nil"), "ProposerDuties")
	}

	return resp.Data, nil
}

// fetchProposal fetches a proposal from a single client and records metrics
func (gc *GoClient) fetchProposal(
	ctx context.Context,
	client Client,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	reqStart := time.Now()
	resp, err := client.Proposal(ctx, &api.ProposalOpts{
		Slot:         slot,
		RandaoReveal: sig,
		Graffiti:     graffiti,
	})
	recordRequest(ctx, gc.log, "Proposal", client, http.MethodGet, false, time.Since(reqStart), err)
	if err != nil {
		return nil, errSingleClient(fmt.Errorf("fetch proposal: %w", err), client.Address(), "Proposal")
	}
	if resp == nil {
		return nil, errSingleClient(fmt.Errorf("proposal response is nil"), client.Address(), "Proposal")
	}
	if resp.Data == nil {
		return nil, errSingleClient(fmt.Errorf("proposal response data is nil"), client.Address(), "Proposal")
	}

	return resp.Data, nil
}

// GetBeaconBlock implements ProposerCalls.GetBeaconBlock
func (gc *GoClient) GetBeaconBlock(
	ctx context.Context,
	slot phase0.Slot,
	graffitiBytes []byte,
	randao []byte,
) (*api.VersionedProposal, ssz.Marshaler, error) {
	// Enrich logger with duty ID if available in context.
	logger := gc.log
	if dutyID, ok := traces.DutyIDFromContext(ctx); ok {
		logger = logger.With(fields.DutyID(dutyID))
	}

	sig := phase0.BLSSignature{}
	copy(sig[:], randao[:])

	graffiti := [32]byte{}
	copy(graffiti[:], graffitiBytes[:])

	var beaconBlock *api.VersionedProposal
	var err error

	// For single client, use direct call to avoid multi-client overhead
	if len(gc.clients) == 1 {
		beaconBlock, err = gc.fetchProposal(ctx, gc.clients[0], slot, sig, graffiti)
		if err != nil {
			return nil, nil, err
		}
	} else {
		// For multiple clients, race them in parallel for the fastest response
		beaconBlock, err = gc.getProposalParallel(ctx, logger, slot, sig, graffiti)
		if err != nil {
			return nil, nil, err
		}
	}

	// Check and log if fee recipient is missing (for both single and multi-client paths)
	feeRecipient, err := beaconBlock.FeeRecipient()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get fee recipient: %w", err)
	}
	if feeRecipient.IsZero() {
		logger.Warn("proposal missing fee recipient - fees will be burned",
			fields.Slot(slot),
			zap.Bool("blinded", beaconBlock.Blinded))
	}

	// Note: FeeRecipient() above already validates payload presence (ExecutionPayload/ExecutionPayloadHeader),
	// so we don't need explicit payload checks in this switch statement
	switch beaconBlock.Version {
	case spec.DataVersionCapella:
		if beaconBlock.Blinded {
			return beaconBlock, beaconBlock.CapellaBlinded, nil
		}
		return beaconBlock, beaconBlock.Capella, nil
	case spec.DataVersionDeneb:
		if beaconBlock.Blinded {
			return beaconBlock, beaconBlock.DenebBlinded, nil
		}
		return beaconBlock, beaconBlock.Deneb, nil
	case spec.DataVersionElectra:
		if beaconBlock.Blinded {
			return beaconBlock, beaconBlock.ElectraBlinded, nil
		}
		return beaconBlock, beaconBlock.Electra, nil
	case spec.DataVersionFulu:
		if beaconBlock.Blinded {
			return beaconBlock, beaconBlock.FuluBlinded, nil
		}
		return beaconBlock, beaconBlock.Fulu, nil
	default:
		return nil, nil, spectypes.WrapError(spectypes.UnknownBlockVersionErrorCode, fmt.Errorf("unknown block version %d", beaconBlock.Version))
	}
}

// getProposalParallel races all beacon nodes and collects proposals for a short time
// and returns the best one according to our score function.
// If no valid proposals are collected in this time it returns the first valid one
// it sees.
//
// This minimizes latency for time-critical block proposals, while still affording
// some time for selecting maximally profitable proposals. Remaining requests are
// canceled immediately to reduce load.
//
// Note: We used to prioritize speed over fee recipient validation - returning
// the first response rather than waiting to compare fee recipients, as missing
// a proposal slot is worse than a nil fee recipient.
// However, it has been observed that the first proposal is usually not the most
// profitable, so we added a little slack time to collect proposals.
//
// The parent context (from duty runner, bounded by slot timing) serves as the hard
// deadline. We never give up early on getting a block proposal - missing a proposal
// is catastrophic, so we wait as long as the slot allows.
func (gc *GoClient) getProposalParallel(
	ctx context.Context,
	logger *zap.Logger,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	// Create a context for the collection period - during this time we gather
	// proposals from multiple beacon nodes to select the best one.
	// After this expires, we return the best seen so far or wait for the first valid one.
	softCtx, cancelSoft := context.WithTimeout(ctx, gc.proposalSoftTimeout)
	defer cancelSoft()

	// Note: We use the parent context (ctx) as the hard deadline, not a separate timeout.
	// The parent context is bounded by the duty runner's slot timing, ensuring we never
	// give up prematurely on getting a block proposal.

	type result struct {
		proposal *api.VersionedProposal
		err      error
		client   string
	}

	resultCh := make(chan result, len(gc.clients))

	for _, client := range gc.clients {
		go func(c Client) {
			proposal, err := gc.fetchProposal(ctx, c, slot, sig, graffiti)
			select {
			case resultCh <- result{proposal: proposal, err: err, client: c.Address()}:
			case <-ctx.Done():
				// Context canceled, exit without blocking
			}
		}(client)
	}

	var errs error
	var bestProposal *api.VersionedProposal
	var bestScore float64
	var bestClient string

	startCollect := time.Now()
	pendingClients := len(gc.clients)
collect:
	for pendingClients > 0 {
		select {
		case res := <-resultCh:
			pendingClients--

			if res.err != nil {
				errs = errors.Join(errs, res.err)
				continue
			}

			proposalScore := gc.scoreProposal(res.proposal)
			logger.Debug("received proposal",
				zap.String("client", res.client),
				zap.Float64("score", proposalScore),
				zap.Duration("latency", time.Since(startCollect)),
				zap.Int("pending", pendingClients),
				zap.Bool("blinded", res.proposal.Blinded),
				fields.Slot(slot),
			)

			if bestProposal == nil ||
				proposalScore > bestScore ||
				// this condition prefers the blinded proposal even if same score
				// as the best we have observed so far
				(res.proposal.Blinded && proposalScore == bestScore) {
				bestProposal = res.proposal
				bestScore = proposalScore
				bestClient = res.client
			}

			if res.proposal.Blinded {
				// We immediately return as an optimization, under the assumption
				// that this is a MEV block; it is a reasonable assumption to make in
				// the usual operating environment.
				// Returning as soon as we fetch at least 1 MEV block is good enough,
				// see https://github.com/ssvlabs/ssv/pull/2631#issuecomment-3678879204
				// Note: We may want to add an operator option to disable this behavior
				// in the future.
				break collect
			}

		case <-softCtx.Done():
			// we are done collecting;
			break collect
		}
	}

	// at this point if we have a proposal, we can just return it, it is the
	// best one we've seen
	if bestProposal != nil {
		logger.Debug("selected best proposal",
			zap.String("client", bestClient),
			zap.Float64("score", bestScore),
			zap.Bool("blinded", bestProposal.Blinded),
			fields.Slot(slot),
		)

		return bestProposal, nil
	}

	logger.Debug("did not receive any valid proposals during the collection period",
		zap.Int("clients", len(gc.clients)),
		zap.Int("pending", pendingClients),
		fields.Slot(slot),
	)

	// there are potentially still some collectors running, just return the first valid one
	for pendingClients > 0 {
		select {
		case res := <-resultCh:
			pendingClients--

			if res.err != nil {
				errs = errors.Join(errs, res.err)
				continue
			}

			// Got a successful response, cancel other requests and return.
			proposalScore := gc.scoreProposal(res.proposal)
			logger.Debug("received proposal; selected first proposal",
				zap.String("client", res.client),
				zap.Float64("score", proposalScore),
				zap.Duration("latency", time.Since(startCollect)),
				zap.Int("pending", pendingClients),
				zap.Bool("blinded", res.proposal.Blinded),
				fields.Slot(slot),
			)
			return res.proposal, nil

		case <-ctx.Done():
			// Parent context canceled (duty deadline reached)
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("all %d clients failed to get proposal for slot %d, encountered errors: %w", len(gc.clients), slot, errs)
}

// scoreProposal computes a score for a beacon proposal.
// see https://github.com/attestantio/vouch/blob/master/strategies/beaconblockproposal/best/score.go as well
func (gc *GoClient) scoreProposal(
	proposal *api.VersionedProposal,
) float64 {
	score, _ := new(big.Int).Add(proposal.ConsensusValue, proposal.ExecutionValue).Float64()
	return score
}

// SubmitBeaconBlock submit the block to the node
func (gc *GoClient) SubmitBeaconBlock(
	ctx context.Context,
	block *api.VersionedProposal,
	sig phase0.BLSSignature,
) error {
	if block.Blinded {
		return gc.submitBlindedBlock(ctx, block, sig)
	}
	return gc.submitRegularBlock(ctx, block, sig)
}

// submitBlindedBlock handles submission of blinded blocks
func (gc *GoClient) submitBlindedBlock(
	ctx context.Context,
	block *api.VersionedProposal,
	sig phase0.BLSSignature,
) error {
	version := block.Version
	signedBlindedBlock := &api.VersionedSignedBlindedProposal{
		Version: version,
	}
	switch version {
	case spec.DataVersionCapella:
		if block.CapellaBlinded == nil {
			return fmt.Errorf("%s blinded block is nil", version.String())
		}
		signedBlindedBlock.Capella = &apiv1capella.SignedBlindedBeaconBlock{
			Message:   block.CapellaBlinded,
			Signature: sig,
		}
	case spec.DataVersionDeneb:
		if block.DenebBlinded == nil {
			return fmt.Errorf("%s blinded block is nil", version.String())
		}
		if block.DenebBlinded.Body == nil {
			return fmt.Errorf("%s blinded block body is nil", version.String())
		}
		if block.DenebBlinded.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("%s blinded block execution payload header is nil", version.String())
		}
		signedBlindedBlock.Deneb = &apiv1deneb.SignedBlindedBeaconBlock{
			Message:   block.DenebBlinded,
			Signature: sig,
		}
	case spec.DataVersionElectra:
		if block.ElectraBlinded == nil {
			return fmt.Errorf("%s blinded block is nil", version.String())
		}
		if block.ElectraBlinded.Body == nil {
			return fmt.Errorf("%s blinded block body is nil", version.String())
		}
		if block.ElectraBlinded.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("%s blinded block execution payload header is nil", version.String())
		}
		signedBlindedBlock.Electra = &apiv1electra.SignedBlindedBeaconBlock{
			Message:   block.ElectraBlinded,
			Signature: sig,
		}
	case spec.DataVersionFulu:
		if block.FuluBlinded == nil {
			return fmt.Errorf("%s blinded block is nil", version.String())
		}
		if block.FuluBlinded.Body == nil {
			return fmt.Errorf("%s blinded block body is nil", version.String())
		}
		if block.FuluBlinded.Body.ExecutionPayloadHeader == nil {
			return fmt.Errorf("%s blinded block execution payload header is nil", version.String())
		}
		// Fulu reuses Electra's block types as per consensus spec
		signedBlindedBlock.Fulu = &apiv1electra.SignedBlindedBeaconBlock{
			Message:   block.FuluBlinded,
			Signature: sig,
		}
	default:
		return fmt.Errorf("unknown blinded block version %d", version)
	}

	opts := &api.SubmitBlindedProposalOpts{
		Proposal: signedBlindedBlock,
	}

	return gc.multiClientSubmit(ctx, "SubmitBlindedProposal", func(ctx context.Context, client Client) error {
		return client.SubmitBlindedProposal(ctx, opts)
	})
}

// submitRegularBlock handles submission of regular (non-blinded) blocks
func (gc *GoClient) submitRegularBlock(
	ctx context.Context,
	block *api.VersionedProposal,
	sig phase0.BLSSignature,
) error {
	version := block.Version
	signedBlock := &api.VersionedSignedProposal{
		Version: version,
	}
	switch version {
	case spec.DataVersionCapella:
		if block.Capella == nil {
			return fmt.Errorf("%s block is nil", version.String())
		}
		signedBlock.Capella = &capella.SignedBeaconBlock{
			Message:   block.Capella,
			Signature: sig,
		}
	case spec.DataVersionDeneb:
		if block.Deneb == nil {
			return fmt.Errorf("%s block contents is nil", version.String())
		}
		if block.Deneb.Block == nil {
			return fmt.Errorf("%s block is nil", version.String())
		}
		if block.Deneb.Block.Body == nil {
			return fmt.Errorf("%s block body is nil", version.String())
		}
		if block.Deneb.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("%s block execution payload is nil", version.String())
		}
		signedBlock.Deneb = &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{
				Message:   block.Deneb.Block,
				Signature: sig,
			},
			KZGProofs: block.Deneb.KZGProofs,
			Blobs:     block.Deneb.Blobs,
		}
	case spec.DataVersionElectra:
		if block.Electra == nil {
			return fmt.Errorf("%s block contents is nil", version.String())
		}
		if block.Electra.Block == nil {
			return fmt.Errorf("%s block is nil", version.String())
		}
		if block.Electra.Block.Body == nil {
			return fmt.Errorf("%s block body is nil", version.String())
		}
		if block.Electra.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("%s block execution payload is nil", version.String())
		}
		signedBlock.Electra = &apiv1electra.SignedBlockContents{
			SignedBlock: &electra.SignedBeaconBlock{
				Message:   block.Electra.Block,
				Signature: sig,
			},
			KZGProofs: block.Electra.KZGProofs,
			Blobs:     block.Electra.Blobs,
		}
	case spec.DataVersionFulu:
		if block.Fulu == nil {
			return fmt.Errorf("%s block contents is nil", version.String())
		}
		if block.Fulu.Block == nil {
			return fmt.Errorf("%s block is nil", version.String())
		}
		if block.Fulu.Block.Body == nil {
			return fmt.Errorf("%s block body is nil", version.String())
		}
		if block.Fulu.Block.Body.ExecutionPayload == nil {
			return fmt.Errorf("%s block execution payload is nil", version.String())
		}
		signedBlock.Fulu = &apiv1fulu.SignedBlockContents{
			// Fulu reuses Electra's block types as per consensus spec
			SignedBlock: &electra.SignedBeaconBlock{
				Message:   block.Fulu.Block,
				Signature: sig,
			},
			KZGProofs: block.Fulu.KZGProofs,
			Blobs:     block.Fulu.Blobs,
		}
	default:
		return spectypes.WrapError(spectypes.UnknownBlockVersionErrorCode, fmt.Errorf("unknown block version %d", version))
	}

	opts := &api.SubmitProposalOpts{
		Proposal: signedBlock,
	}

	return gc.multiClientSubmit(ctx, "SubmitProposal", func(ctx context.Context, client Client) error {
		return client.SubmitProposal(ctx, opts)
	})
}

func (gc *GoClient) SubmitProposalPreparations(
	ctx context.Context,
	preparations []*eth2apiv1.ProposalPreparation,
) error {
	return gc.submitProposalPreparationBatches(preparations, func(batch []*eth2apiv1.ProposalPreparation) error {
		return gc.multiClientSubmit(ctx, "SubmitProposalPreparations", func(ctx context.Context, client Client) error {
			return client.SubmitProposalPreparations(ctx, batch)
		})
	})
}

// handleProposalPreparationsOnReconnect re-submits proposal preparations when a beacon client reconnects.
// This ensures validators can propose blocks even if the beacon node restarted and lost its in-memory
// preparation cache. Called only on reconnection, not on initial connection, to avoid duplicate submissions.
func (gc *GoClient) handleProposalPreparationsOnReconnect(ctx context.Context, client Client, logger *zap.Logger) {
	gc.proposalPreparationsProviderMu.RLock()
	provider := gc.proposalPreparationsProvider
	gc.proposalPreparationsProviderMu.RUnlock()

	// Provider may be nil during early reconnections if the beacon client reconnects before operator.New()
	// completes and calls SetProposalPreparationsProvider. This is harmless - we skip re-submission and let
	// the regular schedule handle it once the fee recipient controller starts.
	if provider == nil {
		logger.Debug("proposal preparations provider not set during reconnection",
			zap.String("reason", "early reconnection before initialization complete"),
			zap.String("impact", "skipping preparation re-submission for this reconnection"))
		return
	}

	preparations, err := provider()
	if err != nil {
		logger.Warn("failed to get preparations from provider on reconnect", zap.Error(err))
		return
	}

	if len(preparations) == 0 {
		return
	}

	err = gc.submitProposalPreparationBatches(preparations, func(batch []*eth2apiv1.ProposalPreparation) error {
		return client.SubmitProposalPreparations(ctx, batch)
	})
	if err != nil {
		logger.Warn("failed to submit proposal preparations on reconnect", zap.Error(err))
		return
	}

	logger.Debug("successfully submitted all proposal preparations on reconnect",
		zap.Int("total", len(preparations)),
	)
}

func (gc *GoClient) submitProposalPreparationBatches(
	preparations []*eth2apiv1.ProposalPreparation,
	submitFunc func(batch []*eth2apiv1.ProposalPreparation) error,
) (jointErr error) {
	var submitted, batchStart int
	for batch := range slices.Chunk(preparations, ProposalPreparationBatchSize) {
		if err := submitFunc(batch); err != nil {
			jointErr = errors.Join(jointErr, fmt.Errorf("submit batch (start=%d, size=%d): %w", batchStart, len(batch), err))
		} else {
			submitted += len(batch)
		}
		batchStart += len(batch)
	}

	switch {
	case submitted == len(preparations):
		return nil
	case submitted > 0:
		return fmt.Errorf("partially submitted proposal preparations: %d/%d, encountered errors: %w", submitted, len(preparations), jointErr)
	default:
		return fmt.Errorf("failed to submit any of %d proposal preparations: %w", len(preparations), jointErr)
	}
}
