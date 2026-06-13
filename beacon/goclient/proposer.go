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

// useSlotRelativeFetch reports whether the operator opted into the MEV-optimized slot-relative
// block-fetch path, signaled by a positive ProposalSoftDeadline. Otherwise the legacy
// relative-timeout path is used.
func (gc *GoClient) useSlotRelativeFetch() bool {
	return gc.proposalSoftDeadline > 0
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

	// For single client, use direct call to avoid multi-client overhead.
	if len(gc.clients) == 1 {
		beaconBlock, err = gc.fetchProposal(ctx, gc.clients[0], slot, sig, graffiti)
		if err != nil {
			return nil, nil, err
		}
		// On the MEV-optimized path, hold the fetched block until the slot-relative deadline so a
		// single-BN operator starts QBFT at the same slot time as multi-BN operators in the cluster
		// (which reach the same deadline via their collection window). The legacy path has no such
		// floor. See docs/MEV_CONSIDERATIONS.md.
		if gc.useSlotRelativeFetch() {
			if err = gc.waitUntilProposalSoftDeadline(ctx, slot); err != nil {
				return nil, nil, err
			}
		}
	} else {
		// For multiple clients, race them in parallel. useSlotRelativeFetch selects the strategy:
		// the MEV-optimized slot-relative window (collect to the deadline, pick the best bid), or
		// the legacy relative timeout.
		if gc.useSlotRelativeFetch() {
			beaconBlock, err = gc.getProposalParallelByDeadline(ctx, logger, slot, sig, graffiti)
		} else {
			beaconBlock, err = gc.getProposalParallelLegacy(ctx, logger, slot, sig, graffiti)
		}
		if err != nil {
			return nil, nil, err
		}
	}

	// Verify proposal parent root against cached HeadEvent (observability only).
	gc.verifyProposalParent(ctx, logger, slot, beaconBlock)

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

// getProposalParallelLegacy implements the legacy block-fetch path — preserved
// bit-for-bit from the pre-path-split code for backward-compat with operators using
// ProposerDelay / ProposalSoftTimeout.
//
// Races all beacon nodes, collects proposals for a short relative-duration timeout
// (gc.proposalSoftTimeout), and returns the best one according to our score function.
// Early-exits on the first blinded response (assumes blinded == MEV). If no valid
// proposals are collected by the soft timeout, returns the first valid one received.
//
// The parent context (from duty runner, bounded by slot timing) serves as the hard
// deadline.
func (gc *GoClient) getProposalParallelLegacy(
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

	if errs == nil {
		return nil, fmt.Errorf("all %d clients failed to get proposal for slot %d", len(gc.clients), slot)
	}
	return nil, fmt.Errorf("all %d clients failed to get proposal for slot %d, encountered errors: %w", len(gc.clients), slot, errs)
}

// proposalFetchResult bundles the outcome of a single per-BN fetch goroutine.
type proposalFetchResult struct {
	proposal *api.VersionedProposal
	err      error
	client   string
}

// spawnProposalFetchers starts a goroutine per beacon-node client; each goroutine
// fetches a proposal and writes its result to the returned channel. Used by the
// MEV-optimized block-fetch implementation.
//
// The channel is buffered to `len(gc.clients)` so each goroutine can write without
// blocking even if the consumer has already returned.
func (gc *GoClient) spawnProposalFetchers(
	ctx context.Context,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) <-chan proposalFetchResult {
	resultCh := make(chan proposalFetchResult, len(gc.clients))
	for _, client := range gc.clients {
		go func(c Client) {
			proposal, err := gc.fetchProposal(ctx, c, slot, sig, graffiti)
			select {
			case resultCh <- proposalFetchResult{proposal: proposal, err: err, client: c.Address()}:
			case <-ctx.Done():
				// Context canceled, exit without blocking.
			}
		}(client)
	}
	return resultCh
}

// waitForFirstValidProposal returns the first valid proposal received from the
// remaining in-flight fetchers. Used by the MEV-optimized path as the fallback
// after the soft-deadline collection window has elapsed without producing a usable
// best proposal. Bounded by the parent context's slot deadline.
func (gc *GoClient) waitForFirstValidProposal(
	ctx context.Context,
	logger *zap.Logger,
	slot phase0.Slot,
	startCollect time.Time,
	resultCh <-chan proposalFetchResult,
	pendingClients int,
	errs error,
) (*api.VersionedProposal, error) {
	for pendingClients > 0 {
		select {
		case res := <-resultCh:
			pendingClients--
			if res.err != nil {
				errs = errors.Join(errs, res.err)
				continue
			}
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
			// Preserve any accumulated BN failure context alongside the parent
			// deadline error — operators need both to diagnose missed slots.
			return nil, errors.Join(ctx.Err(), errs)
		}
	}
	return nil, fmt.Errorf("all %d clients failed to get proposal for slot %d, encountered errors: %w", len(gc.clients), slot, errs)
}

// waitUntilProposalSoftDeadline blocks until the slot-relative proposal soft deadline
// (slot_start + gc.proposalSoftDeadline) for the given slot, or until ctx is canceled. Returns
// immediately if the deadline has already passed (e.g. after a slow block fetch). Used by the
// single-BN MEV-optimized path to align QBFT start with multi-BN operators. See docs/MEV_CONSIDERATIONS.md.
func (gc *GoClient) waitUntilProposalSoftDeadline(ctx context.Context, slot phase0.Slot) error {
	deadline := gc.getBeaconConfig().SlotStartTime(slot).Add(gc.proposalSoftDeadline)
	wait := time.Until(deadline)
	if wait <= 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getProposalParallelByDeadline implements the MEV-optimized slot-relative-deadline parallel
// block-fetch (multi-BN).
//
// Spawns a per-BN fetch in parallel and collects responses until the slot-relative
// ProposalSoftDeadline (slot_start + gc.proposalSoftDeadline) fires — deliberately *without*
// early-exiting on the first blinded response, so that (a) the best-scored bid across BNs can be
// selected and (b) QBFT starts at the same slot-relative time across the cluster. It bails out
// before the deadline only if every BN has responded and none produced a usable proposal (waiting
// out the deadline cannot then conjure one).
//
// After the deadline, returns the best-scored proposal collected so far, or falls through to
// waitForFirstValidProposal if nothing usable arrived. The parent ctx serves as the hard deadline.
//
// Note: in-flight BN fetches are spawned with the parent ctx (not softCtx), so a slow BN's HTTP
// call may keep running after we return — until the duty's slot deadline cancels ctx. The
// fetchProposal call's own HTTP timeouts bound the worst case.
func (gc *GoClient) getProposalParallelByDeadline(
	ctx context.Context,
	logger *zap.Logger,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	// Slot-relative deadline: fires at slot_start + ProposalSoftDeadline regardless
	// of when this function is invoked.
	slotStart := gc.getBeaconConfig().SlotStartTime(slot)
	softCtx, cancelSoft := context.WithDeadline(ctx, slotStart.Add(gc.proposalSoftDeadline))
	defer cancelSoft()

	resultCh := gc.spawnProposalFetchers(ctx, slot, sig, graffiti)

	var errs error
	var bestProposal *api.VersionedProposal
	var bestScore float64
	var bestClient string

	startCollect := time.Now()
	pendingClients := len(gc.clients)
collect:
	for {
		select {
		case res := <-resultCh:
			pendingClients--

			if res.err != nil {
				errs = errors.Join(errs, res.err)
				// If every client has responded and none produced a usable block, stop now —
				// waiting out the deadline cannot conjure a proposal. (With a usable block in
				// hand we keep waiting until the deadline below, to align QBFT start.)
				if pendingClients == 0 && bestProposal == nil {
					break collect
				}
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
				// prefer the blinded proposal even if same score as the best so far
				(res.proposal.Blinded && proposalScore == bestScore) {
				bestProposal = res.proposal
				bestScore = proposalScore
				bestClient = res.client
			}

			// No early-exit on blinded: we keep collecting until the slot-relative deadline even
			// once we hold a (blinded/MEV) block, to compare bids across BNs and to align QBFT
			// start across the cluster. See docs/MEV_CONSIDERATIONS.md.

		case <-softCtx.Done():
			break collect
		}
	}

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

	return gc.waitForFirstValidProposal(ctx, logger, slot, startCollect, resultCh, pendingClients, errs)
}

// scoreProposal computes a score for a beacon proposal.
// see https://github.com/attestantio/vouch/blob/master/strategies/beaconblockproposal/best/score.go as well
func (gc *GoClient) scoreProposal(
	proposal *api.VersionedProposal,
) float64 {
	score, _ := new(big.Int).Add(proposal.ConsensusValue, proposal.ExecutionValue).Float64()
	return score
}

// verifyProposalParent checks the proposal's parent root against cached HeadEvent.
// This is observability only - no re-fetch, just metrics and logging.
func (gc *GoClient) verifyProposalParent(
	ctx context.Context,
	logger *zap.Logger,
	slot phase0.Slot,
	proposal *api.VersionedProposal,
) {
	proposalParentVerifyCounter.Add(ctx, 1)

	parentRoot, err := proposal.ParentRoot()
	if err != nil {
		logger.Warn("failed to get proposal parent root", fields.Slot(slot), zap.Error(err))
		return
	}

	// Parent is from slot-1
	parentSlot := slot - 1
	item := gc.headCache.Get(parentSlot)
	if item == nil {
		proposalParentCacheMissCounter.Add(ctx, 1)
		return
	}
	expectedRoot := item.Value()

	if parentRoot == expectedRoot {
		proposalParentMatchCounter.Add(ctx, 1)
		return
	}

	proposalParentMismatchCounter.Add(ctx, 1)
	logger.Warn("proposal parent root mismatch detected",
		fields.Slot(slot),
		zap.Uint64("parent_slot", uint64(parentSlot)),
		zap.Stringer("expected_root", expectedRoot),
		zap.Stringer("got_root", parentRoot),
	)
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
