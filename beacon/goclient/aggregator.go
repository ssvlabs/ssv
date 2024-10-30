package goclient

import (
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
)

// SubmitAggregateSelectionProof returns an AggregateAndProof object
func (gc *GoClient) SubmitAggregateSelectionProof(slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	// As specified in spec, an aggregator should wait until two thirds of the way through slot
	// to broadcast the best aggregate to the global aggregate channel.
	// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/validator/0_beacon-chain-validator.md#broadcast-aggregate
	gc.waitToSlotTwoThirds(slot)

	attData, _, err := gc.GetAttestationData(slot, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data: %w", err)
	}
	if attData == nil {
		return nil, DataVersionNil, fmt.Errorf("attestation data is nil")
	}

	// Get aggregate attestation data.
	root, err := attData.HashTreeRoot()
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data root: %w", err)
	}

	aggDataReqStart := time.Now()
	aggDataResp, err := gc.client.AggregateAttestation(gc.ctx, &api.AggregateAttestationOpts{
		Slot:                slot,
		AttestationDataRoot: root,
	})
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get aggregate attestation: %w", err)
	}
	if aggDataResp == nil {
		return nil, DataVersionNil, fmt.Errorf("aggregate attestation response is nil")
	}
	if aggDataResp.Data == nil {
		return nil, DataVersionNil, fmt.Errorf("aggregate attestation data is nil")
	}

	metricsAggregatorDataRequest.Observe(time.Since(aggDataReqStart).Seconds())

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	return &phase0.AggregateAndProof{
		AggregatorIndex: index,
		Aggregate:       aggDataResp.Data,
		SelectionProof:  selectionProof,
	}, spec.DataVersionPhase0, nil
}

// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
func (gc *GoClient) SubmitSignedAggregateSelectionProof(msg *phase0.SignedAggregateAndProof) error {
	return gc.client.SubmitAggregateAttestations(gc.ctx, []*phase0.SignedAggregateAndProof{msg})
}

// waitToSlotTwoThirds waits until two-third of the slot has transpired (SECONDS_PER_SLOT * 2 / 3 seconds after the start of slot)
func (gc *GoClient) waitToSlotTwoThirds(slot phase0.Slot) {
	oneThird := gc.network.SlotDurationSec() / 3 /* one third of slot duration */

	finalTime := gc.slotStartTime(slot).Add(2 * oneThird)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}
	time.Sleep(wait)
}
