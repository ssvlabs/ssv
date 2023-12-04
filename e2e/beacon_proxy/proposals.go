package beaconproxy

import (
	"encoding/json"
	"fmt"
	"net/http"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (b *BeaconProxy) handleProposerDuties(w http.ResponseWriter, r *http.Request) {
	logger, gateway := b.requestContext(r)

	// Parse request.
	var epoch phase0.Epoch
	if chi.URLParam(r, "epoch") != "" {
		if _, err := fmt.Sscanf(chi.URLParam(r, "epoch"), "%d", &epoch); err != nil {
			b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
			return
		}
	}
	indices, err := parseIndicesFromRequest(r, false)
	if err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to read request: %w", err))
		return
	}

	// Obtain duties.
	duties, err := b.client.(eth2client.ProposerDutiesProvider).ProposerDuties(
		r.Context(),
		epoch,
		indices,
	)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain proposer duties: %w", err))
		return
	}

	// Intercept.
	duties, err = gateway.Interceptor.InterceptProposerDuties(r.Context(), logger, epoch, indices, duties)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to intercept proposer duties: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, duties); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	logger.Info("obtained proposer duties",
		zap.Uint64("epoch", uint64(epoch)),
		zap.Int("indices", len(indices)),
	)
}

func (b *BeaconProxy) handleBlockProposal(w http.ResponseWriter, r *http.Request) {
	logger, gateway := b.requestContext(r)

	// Parse request.
	var (
		slot         phase0.Slot
		randaoReveal []byte
		graffiti     []byte
	)
	if err := scanURL(r, "slot:%d", &slot, "randao_reveal:%x", &randaoReveal, "graffiti:%x", &graffiti); err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	// Obtain block.
	versionedBlock, err := b.client.(eth2client.BeaconBlockProposalProvider).BeaconBlockProposal(
		r.Context(),
		slot,
		phase0.BLSSignature(randaoReveal),
		graffiti,
	)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain block: %w", err))
		return
	}
	var block any
	switch versionedBlock.Version {
	case spec.DataVersionCapella:
		block = versionedBlock.Capella
	default:
		b.error(r, w, 500, fmt.Errorf("unsupported block version %d", versionedBlock.Version))
		return
	}

	// Intercept.
	block, err = gateway.Interceptor.InterceptBlockProposal(
		r.Context(),
		logger,
		slot,
		phase0.BLSSignature(randaoReveal),
		graffiti,
		block.(*spec.VersionedBeaconBlock),
	)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to intercept block: %w", err))
		return
	}

	// Respond.
	var response = struct {
		Version spec.DataVersion `json:"version"`
		Data    any              `json:"data"`
	}{
		Version: versionedBlock.Version,
		Data:    block,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	logger.Info("obtained block",
		zap.Uint64("slot", uint64(slot)),
		zap.String("randao_reveal", fmt.Sprintf("%x", randaoReveal)),
		zap.String("graffiti", fmt.Sprintf("%x", graffiti)),
	)
}

func (b *BeaconProxy) handleSubmitBlockProposal(w http.ResponseWriter, r *http.Request) {
	logger, gateway := b.requestContext(r)

	// Parse request.
	var block *capella.SignedBeaconBlock
	if err := json.NewDecoder(r.Body).Decode(&block); err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	// Intercept.
	versionedBlock := &spec.VersionedSignedBeaconBlock{
		Version: spec.DataVersionCapella,
		Capella: block,
	}
	versionedBlock, err := gateway.Interceptor.InterceptSubmitBlockProposal(
		r.Context(),
		logger,
		versionedBlock,
	)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to intercept block: %w", err))
		return
	}

	// Submit.
	if err := b.client.(eth2client.BeaconBlockSubmitter).SubmitBeaconBlock(r.Context(), versionedBlock); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to submit block: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, nil); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	logger.Info("submitted block",
		zap.Uint64("slot", uint64(block.Message.Slot)),
	)
}
