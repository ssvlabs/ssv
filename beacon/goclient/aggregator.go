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

	"github.com/ssvlabs/ssv/networkconfig"
)

// IsAggregator returns true if the validator is selected as an aggregator for the given
// slot/committee, per the selection-proof modulo check.
func (gc *GoClient) IsAggregator(
	_ context.Context,
	_ phase0.Slot,
	_ phase0.CommitteeIndex,
	committeeLength uint64,
	slotSig []byte,
) bool {
	return networkconfig.IsAggregatorSelected(gc.beaconConfig.TargetAggregatorsPerCommittee, committeeLength, slotSig)
}

// GetAggregateAttestation returns the aggregate attestation for the given slot and committee.
func (gc *GoClient) GetAggregateAttestation(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
) (ssz.Marshaler, spec.DataVersion, error) {
	va, _, err := gc.fetchVersionedAggregate(ctx, slot, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, err
	}
	return versionedAggregateToSSZ(va)
}

// SubmitAggregateSelectionProof returns an AggregateAndProof object
func (gc *GoClient) SubmitAggregateSelectionProof(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	_ uint64,
	index phase0.ValidatorIndex,
	slotSig []byte,
) (ssz.Marshaler, spec.DataVersion, error) {
	// As specified in spec, an aggregator waits until the aggregation deadline (see
	// waitIntoSlot) to broadcast the best aggregate to the global aggregate channel.
	// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/validator/0_beacon-chain-validator.md#broadcast-aggregate
	if err := gc.waitIntoSlot(ctx, slot, 2); err != nil {
		return nil, 0, fmt.Errorf("wait for aggregation deadline: %w", err)
	}

	va, _, err := gc.fetchVersionedAggregate(ctx, slot, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, err
	}

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	return versionedToAggregateAndProof(va, index, selectionProof)
}

// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
func (gc *GoClient) SubmitSignedAggregateSelectionProof(
	ctx context.Context,
	msg *spec.VersionedSignedAggregateAndProof,
) error {
	start := time.Now()
	err := gc.multiClient.SubmitAggregateAttestations(ctx, &api.SubmitAggregateAttestationsOpts{SignedAggregateAndProofs: []*spec.VersionedSignedAggregateAndProof{msg}})
	recordRequest(ctx, gc.log, "SubmitAggregateAttestations", gc.multiClient, http.MethodPost, true, time.Since(start), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit aggregate attestations: %w", err), "SubmitAggregateAttestations")
	}

	return nil
}

// waitIntoSlot waits until the given number of intervals into the slot has transpired
// (intervals * IntervalDuration after the start of the slot): intervals=1 is one interval in
// (attestation/contribution deadline), intervals=2 is two intervals in (aggregate broadcast
// deadline). IntervalDuration is 1/3 of the slot before Gloas, 1/4 from Gloas on (SIP #94 §1).
func (gc *GoClient) waitIntoSlot(ctx context.Context, slot phase0.Slot, intervals int) error {
	config := gc.getBeaconConfig()
	finalTime := config.SlotStartTime(slot).Add(time.Duration(intervals) * config.IntervalDuration(slot))
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

// computeAttestationDataRoot re-derives the attestation data root for the given slot/committee
// from this node's own view, used as a fallback when the cluster-attested root is unknown.
func (gc *GoClient) computeAttestationDataRoot(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
) (root [32]byte, err error) {
	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return root, fmt.Errorf("fetch attestation data: %w", err)
	}

	// Explicitly set Index field as beacon nodes may return inconsistent values.
	// EIP-7549: Electra+ uses Index=0; pre-Electra uses committee index. Gloas (EIP-7732) instead keeps
	// the BN-supplied payload-status index (0=EMPTY/1=FULL) — it is part of the signed AttestationData
	// (SIP #94 §2 aggregation path), so the aggregate must be fetched under it.
	// Decide the fork from the requested duty slot, not attData.Slot — the latter is what the
	// beacon node returned (the same source the comment above warns "may return inconsistent values"),
	// whereas the aggregate is for our duty's slot, which is authoritative.
	cfg := gc.getBeaconConfig()
	switch {
	case cfg.IsGloasAtSlot(slot):
		// keep attData.Index as the BN returned it
	default:
		version, _ := cfg.ForkAtEpoch(cfg.EstimatedEpochAtSlot(slot))
		attData.Index = 0
		if version < spec.DataVersionElectra {
			attData.Index = committeeIndex
		}
	}

	root, err = attData.HashTreeRoot()
	if err != nil {
		return root, fmt.Errorf("fetch attestation data root: %w", err)
	}
	return root, nil
}

// fetchVersionedAggregate fetches the aggregate attestation for the given slot/committee,
// shared by SubmitAggregateSelectionProof (AggregatorRunner) and GetAggregateAttestation
// (AggregatorCommitteeRunner).
//
// Prefers the root of the attestation data this node actually submitted for this duty (the
// cluster-decided value): the beacon node then holds at least our own attestation matching it.
// Re-deriving the data locally can yield a root nobody attested with, which the node answers
// with a 404 (no matching aggregate).
func (gc *GoClient) fetchVersionedAggregate(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
) (*spec.VersionedAttestation, spec.DataVersion, error) {
	root, found := gc.attestedDataRoot(slot, committeeIndex)
	if !found {
		// No record of our own attestation (it failed or hasn't landed yet) — fall back
		// to re-deriving the root from this node's view of the slot.
		var err error
		root, err = gc.computeAttestationDataRoot(ctx, slot, committeeIndex)
		if err != nil {
			return nil, DataVersionNil, err
		}
	}

	aggDataReqStart := time.Now()
	aggDataResp, err := gc.multiClient.AggregateAttestation(ctx, &api.AggregateAttestationOpts{
		Slot:                slot,
		AttestationDataRoot: root,
		CommitteeIndex:      committeeIndex,
	})
	recordRequest(ctx, gc.log, "AggregateAttestation", gc.multiClient, http.MethodGet, true, time.Since(aggDataReqStart), err)
	if err != nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("fetch aggregate attestation: %w", err), "AggregateAttestation")
	}
	if err := checkPtrResponse(aggDataResp, "aggregate attestation"); err != nil {
		return nil, DataVersionNil, errMultiClient(err, "AggregateAttestation")
	}

	return aggDataResp.Data, aggDataResp.Data.Version, nil
}

func versionedAggregateToSSZ(va *spec.VersionedAttestation) (ssz.Marshaler, spec.DataVersion, error) {
	switch va.Version {
	case spec.DataVersionPhase0:
		if va.Phase0 == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Phase0, va.Version, nil
	case spec.DataVersionAltair:
		if va.Altair == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Altair, va.Version, nil
	case spec.DataVersionBellatrix:
		if va.Bellatrix == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Bellatrix, va.Version, nil
	case spec.DataVersionCapella:
		if va.Capella == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Capella, va.Version, nil
	case spec.DataVersionDeneb:
		if va.Deneb == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Deneb, va.Version, nil
	case spec.DataVersionElectra:
		if va.Electra == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Electra, va.Version, nil
	case spec.DataVersionFulu:
		if va.Fulu == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return va.Fulu, va.Version, nil
	default:
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("unknown data version: %d", va.Version), "AggregateAttestation")
	}
}

func versionedToAggregateAndProof(
	va *spec.VersionedAttestation,
	index phase0.ValidatorIndex,
	selectionProof phase0.BLSSignature,
) (ssz.Marshaler, spec.DataVersion, error) {
	switch va.Version {
	case spec.DataVersionPhase0:
		if va.Phase0 == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Phase0,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionAltair:
		if va.Altair == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Altair,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionBellatrix:
		if va.Bellatrix == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Bellatrix,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionCapella:
		if va.Capella == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Capella,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionDeneb:
		if va.Deneb == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Deneb,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionElectra:
		if va.Electra == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Electra,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionFulu:
		if va.Fulu == nil {
			return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation %s data is nil", va.Version.String()), "AggregateAttestation")
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Fulu,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("unknown data version: %d", va.Version)
	}
}
