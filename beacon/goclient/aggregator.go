package goclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
)

func (gc *GoClient) IsAggregator(
	_ context.Context,
	_ phase0.Slot,
	_ phase0.CommitteeIndex,
	committeeLength uint64,
	slotSig []byte,
) bool {
	const targetAggregatorsPerCommittee = 16

	modulo := committeeLength / targetAggregatorsPerCommittee
	if modulo == 0 {
		modulo = 1
	}

	h := sha256.Sum256(slotSig)
	x := binary.LittleEndian.Uint64(h[:8])

	return x%modulo == 0
}

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
	err := gc.multiClient.SubmitAggregateAttestations(ctx, &api.SubmitAggregateAttestationsOpts{
		SignedAggregateAndProofs: []*spec.VersionedSignedAggregateAndProof{msg},
	})
	recordRequest(ctx, gc.log, "SubmitAggregateAttestations", gc.multiClient, http.MethodPost, true, time.Since(start), err)
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

func (gc *GoClient) computeAttDataRootAndVersion(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
) (root [32]byte, err error) {
	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return root, fmt.Errorf("fetch attestation data: %w", err)
	}

	// Explicitly set Index field as beacon nodes may return inconsistent values.
	// EIP-7549: Electra+ uses Index=0; pre-Electra uses committee index
	version, _ := gc.beaconConfig.ForkAtEpoch(gc.getBeaconConfig().EstimatedEpochAtSlot(attData.Slot))
	attData.Index = 0
	if version < spec.DataVersionElectra {
		attData.Index = committeeIndex
	}

	root, err = attData.HashTreeRoot()
	if err != nil {
		return root, fmt.Errorf("fetch attestation data root: %w", err)
	}
	return root, nil
}

func (gc *GoClient) fetchVersionedAggregate(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
) (*spec.VersionedAttestation, spec.DataVersion, error) {
	root, err := gc.computeAttDataRootAndVersion(ctx, slot, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("compute attestation root: %w", err), "AggregateAttestation")
	}

	start := time.Now()
	resp, err := gc.multiClient.AggregateAttestation(ctx, &api.AggregateAttestationOpts{
		Slot:                slot,
		AttestationDataRoot: root,
		CommitteeIndex:      committeeIndex,
	})
	recordRequest(ctx, gc.log, "AggregateAttestation", gc.multiClient, http.MethodGet, true, time.Since(start), err)
	if err != nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("fetch aggregate attestation: %w", err), "AggregateAttestation")
	}
	if resp == nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation response is nil"), "AggregateAttestation")
	}
	if resp.Data == nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("aggregate attestation data is nil"), "AggregateAttestation")
	}
	return resp.Data, resp.Data.Version, nil
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
		// Fulu AggregateAndProof usees electra.AggregateAndProof in go-eth2-client
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Fulu,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	default:
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("unknown data version: %d", va.Version), "AggregateAttestation")
	}
}
