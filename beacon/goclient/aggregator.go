package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
)

// SubmitAggregateSelectionProof returns an AggregateAndProof object
func (gc *GoClient) SubmitAggregateSelectionProof(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	committeeLength uint64,
	index phase0.ValidatorIndex,
	slotSig []byte,
) (ssz.Marshaler, spec.DataVersion, error) {
	// As specified in spec, an aggregator should wait until two thirds of the way through slot
	// to broadcast the best aggregate to the global aggregate channel.
	// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/validator/0_beacon-chain-validator.md#broadcast-aggregate
	if err := gc.waitToSlotTwoThirds(ctx, slot); err != nil {
		return nil, 0, fmt.Errorf("wait for 2/3 of slot: %w", err)
	}

	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("fetch attestation data: %w", err)
	}

	dataVersion, _ := gc.beaconConfig.ForkAtEpoch(gc.getBeaconConfig().EstimatedEpochAtSlot(attData.Slot))
	if dataVersion < spec.DataVersionElectra {
		attData.Index = committeeIndex
	}

	// Get aggregate attestation data.
	root, err := attData.HashTreeRoot()
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("fetch attestation data root: %w", err)
	}

	aggDataReqStart := time.Now()
	aggDataResp, err := gc.multiClient.AggregateAttestation(ctx, &api.AggregateAttestationOpts{
		Slot:                slot,
		AttestationDataRoot: root,
		CommitteeIndex:      committeeIndex,
	})
	recordMultiClientRequest(ctx, gc.log, "AggregateAttestation", http.MethodGet, time.Since(aggDataReqStart), err)
	if err != nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("fetch aggregate attestation: %w", err), "AggregateAttestation")
	}
	if aggDataResp == nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation response is nil"), "AggregateAttestation")
	}
	if aggDataResp.Data == nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation response data is nil"), "AggregateAttestation")
	}

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	switch aggDataResp.Data.Version {
	case spec.DataVersionElectra:
		if aggDataResp.Data.Electra == nil {
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation electra data is nil")
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Electra,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionDeneb:
		if aggDataResp.Data.Deneb == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation deneb data is nil"), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Deneb,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionCapella:
		if aggDataResp.Data.Capella == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation capella data is nil"), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Capella,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionBellatrix:
		if aggDataResp.Data.Bellatrix == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation bellatrix data is nil"), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Bellatrix,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionAltair:
		if aggDataResp.Data.Altair == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation altair data is nil"), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Altair,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	default:
		if aggDataResp.Data.Phase0 == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation phase0 data is nil"), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Phase0,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	}
}

// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
func (gc *GoClient) SubmitSignedAggregateSelectionProof(
	ctx context.Context,
	msg *spec.VersionedSignedAggregateAndProof,
) error {
	start := time.Now()
	err := gc.multiClient.SubmitAggregateAttestations(ctx, &api.SubmitAggregateAttestationsOpts{SignedAggregateAndProofs: []*spec.VersionedSignedAggregateAndProof{msg}})
	recordMultiClientRequest(ctx, gc.log, "SubmitAggregateAttestations", http.MethodPost, time.Since(start), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit aggregate attestations: %w", err), "SubmitAggregateAttestations")
	}

	return nil
}

// waitToSlotTwoThirds waits until two-third of the slot has transpired (SECONDS_PER_SLOT * 2 / 3 seconds after the start of slot)
func (gc *GoClient) waitToSlotTwoThirds(ctx context.Context, slot phase0.Slot) error {
	config := gc.getBeaconConfig()
	oneInterval := config.IntervalDuration()
	finalTime := config.SlotStartTime(slot).Add(2 * oneInterval)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}
