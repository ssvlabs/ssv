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
	"go.uber.org/zap"
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

	va, _, err := gc.fetchVersionedAggregate(ctx, slot, committeeIndex)
	if err != nil {
		return nil, DataVersionNil, err
	}

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	return gc.versionedToAggregateAndProof(va, index, selectionProof)
}

// SubmitSignedAggregateSelectionProof broadcasts a signed aggregator msg
func (gc *GoClient) SubmitSignedAggregateSelectionProof(
	ctx context.Context,
	msg *spec.VersionedSignedAggregateAndProof,
) error {
	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitAggregateAttestations"),
		zap.String("client_addr", clientAddress))

	start := time.Now()

	err := gc.multiClient.SubmitAggregateAttestations(ctx, &api.SubmitAggregateAttestationsOpts{SignedAggregateAndProofs: []*spec.VersionedSignedAggregateAndProof{msg}})
	recordRequestDuration(ctx, "SubmitAggregateAttestations", gc.multiClient.Address(), http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted signed aggregate attestations")
	return nil
}

func (gc *GoClient) IsAggregator(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
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
) (ssz.Marshaler, error) {
	va, _, err := gc.fetchVersionedAggregate(ctx, slot, committeeIndex)
	if err != nil {
		return nil, err
	}
	agg, _, err := gc.versionedAggregateToSSZ(va)
	return agg, err
}

func (gc *GoClient) fetchVersionedAggregate(
	ctx context.Context,
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
) (*spec.VersionedAttestation, spec.DataVersion, error) {
	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("get attestation data: %w", err)
	}

	dataVersion, _ := gc.beaconConfig.ForkAtEpoch(gc.getBeaconConfig().EstimatedEpochAtSlot(attData.Slot))
	// Pre-Electra needs AttestationData.Index set
	if dataVersion < spec.DataVersionElectra {
		attData.Index = committeeIndex
	}

	root, err := attData.HashTreeRoot()
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("attestation data root: %w", err)
	}

	start := time.Now()
	resp, err := gc.multiClient.AggregateAttestation(ctx, &api.AggregateAttestationOpts{
		Slot:                slot,
		AttestationDataRoot: root,
		CommitteeIndex:      committeeIndex, // ignored by older forks, used by newer
	})
	recordRequestDuration(ctx, "AggregateAttestation", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "AggregateAttestation"), zap.Error(err))
		return nil, DataVersionNil, fmt.Errorf("aggregate attestation: %w", err)
	}
	if resp == nil || resp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg, zap.String("api", "AggregateAttestation"))
		return nil, DataVersionNil, fmt.Errorf("nil aggregate attestation response")
	}

	return resp.Data, resp.Data.Version, nil
}

func (gc *GoClient) versionedAggregateToSSZ(
	va *spec.VersionedAttestation,
) (ssz.Marshaler, spec.DataVersion, error) {
	switch va.Version {
	case spec.DataVersionElectra:
		if va.Electra == nil {
			return nil, DataVersionNil, fmt.Errorf("electra data is nil")
		}
		return va.Electra, va.Version, nil
	case spec.DataVersionDeneb:
		if va.Deneb == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb data is nil")
		}
		return va.Deneb, va.Version, nil
	case spec.DataVersionCapella:
		if va.Capella == nil {
			return nil, DataVersionNil, fmt.Errorf("capella data is nil")
		}
		return va.Capella, va.Version, nil
	case spec.DataVersionBellatrix:
		if va.Bellatrix == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix data is nil")
		}
		return va.Bellatrix, va.Version, nil
	case spec.DataVersionAltair:
		if va.Altair == nil {
			return nil, DataVersionNil, fmt.Errorf("altair data is nil")
		}
		return va.Altair, va.Version, nil
	default:
		if va.Phase0 == nil {
			return nil, DataVersionNil, fmt.Errorf("phase0 data is nil")
		}
		return va.Phase0, va.Version, nil
	}
}

func (gc *GoClient) versionedToAggregateAndProof(
	va *spec.VersionedAttestation,
	index phase0.ValidatorIndex,
	selectionProof phase0.BLSSignature,
) (ssz.Marshaler, spec.DataVersion, error) {
	switch va.Version {
	case spec.DataVersionElectra:
		if va.Electra == nil {
			return nil, DataVersionNil, fmt.Errorf("electra data is nil")
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Electra,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionDeneb:
		if va.Deneb == nil {
			return nil, DataVersionNil, fmt.Errorf("deneb data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Deneb,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionCapella:
		if va.Capella == nil {
			return nil, DataVersionNil, fmt.Errorf("capella data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Capella,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionBellatrix:
		if va.Bellatrix == nil {
			return nil, DataVersionNil, fmt.Errorf("bellatrix data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Bellatrix,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	case spec.DataVersionAltair:
		if va.Altair == nil {
			return nil, DataVersionNil, fmt.Errorf("altair data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Altair,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	default:
		if va.Phase0 == nil {
			return nil, DataVersionNil, fmt.Errorf("phase0 data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       va.Phase0,
			SelectionProof:  selectionProof,
		}, va.Version, nil
	}
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
