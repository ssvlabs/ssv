package goclient

import (
	"context"
	"errors"
	"fmt"
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
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
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
		beaconBlock, err = gc.getProposalParallel(ctx, slot, sig, graffiti)
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
		gc.log.Warn("proposal missing fee recipient - fees will be burned",
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
		return nil, nil, fmt.Errorf("unknown block version %d", beaconBlock.Version)
	}
}

// getProposalParallel races all beacon nodes and returns the first successful response.
// This minimizes latency for time-critical block proposals. Remaining requests are
// canceled immediately to reduce load.
//
// Note: We prioritize speed over fee recipient validation - returning the first response
// rather than waiting to compare fee recipients, as missing a proposal slot is worse
// than a nil fee recipient.
func (gc *GoClient) getProposalParallel(
	ctx context.Context,
	slot phase0.Slot,
	sig phase0.BLSSignature,
	graffiti [32]byte,
) (*api.VersionedProposal, error) {
	// Create a context that we'll cancel as soon as we get the first successful response
	parallelCtx, cancelParallel := context.WithCancel(ctx)
	defer cancelParallel()

	type result struct {
		proposal *api.VersionedProposal
		err      error
		client   string
	}

	resultCh := make(chan result, len(gc.clients))

	for _, client := range gc.clients {
		go func(c Client) {
			proposal, err := gc.fetchProposal(parallelCtx, c, slot, sig, graffiti)
			select {
			case resultCh <- result{proposal: proposal, err: err, client: c.Address()}:
			case <-parallelCtx.Done():
				// Context canceled, exit without blocking
			}
		}(client)
	}

	var errs error
	for range gc.clients {
		select {
		case res := <-resultCh:
			if res.err != nil {
				errs = errors.Join(errs, res.err)
				continue
			}
			// Got a successful response, cancel other requests and return.
			gc.log.Debug("received proposal, canceling other requests",
				zap.String("client", res.client),
				fields.Slot(slot),
			)
			cancelParallel()
			return res.proposal, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("all %d clients failed to get proposal for slot %d, encountered errors: %w", len(gc.clients), slot, errs)
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
		return fmt.Errorf("unknown block version %d", version)
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
