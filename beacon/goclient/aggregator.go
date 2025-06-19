package goclient

import (
	"context"
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
	gc.waitToSlotTwoThirds(slot)

	// differ from spec because we need to subscribe to subnet
	isAggregator := gc.isAggregator(committeeLength, slotSig)
	if !isAggregator {
		return nil, DataVersionNil, fmt.Errorf("validator is not an aggregator")
	}

	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data: %w", err)
	}

	// dataVersion, _ := gc.beaconConfig.ForkAtEpoch(gc.getBeaconConfig().EstimatedEpochAtSlot(attData.Slot))
	// if dataVersion < spec.DataVersionElectra {
	// 	attData.Index = committeeIndex
	// }

	// Get aggregate attestation data.
	root, err := attData.HashTreeRoot()
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data root: %w", err)
	}

	aggDataReqStart := time.Now()
	aggDataResp, err := gc.multiClient.AggregateAttestation(ctx, &api.AggregateAttestationOpts{
		Slot:                slot,
		AttestationDataRoot: root,
		CommitteeIndex:      committeeIndex,
	})
	recordRequestDuration(ctx, "AggregateAttestation", gc.multiClient.Address(), http.MethodGet, time.Since(aggDataReqStart), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "AggregateAttestation"),
			zap.Error(err),
		)
		return nil, DataVersionNil, fmt.Errorf("failed to get aggregate attestation: %w", err)
	}
	if aggDataResp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "AggregateAttestation"),
		)
		return nil, DataVersionNil, fmt.Errorf("aggregate attestation response is nil")
	}
	if aggDataResp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "AggregateAttestation"),
		)
		return nil, DataVersionNil, fmt.Errorf("aggregate attestation data is nil")
	}

	var selectionProof phase0.BLSSignature
	copy(selectionProof[:], slotSig)

	switch aggDataResp.Data.Version {
	case spec.DataVersionElectra:
		if aggDataResp.Data.Electra == nil {
			gc.log.Error(clNilResponseForkDataErrMsg,
				zap.String("api", "AggregateAttestation"),
			)
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation electra data is nil")
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Electra,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionDeneb:
		if aggDataResp.Data.Deneb == nil {
			gc.log.Error(clNilResponseForkDataErrMsg,
				zap.String("api", "AggregateAttestation"),
			)
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation deneb data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Deneb,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionCapella:
		if aggDataResp.Data.Capella == nil {
			gc.log.Error(clNilResponseForkDataErrMsg,
				zap.String("api", "AggregateAttestation"),
			)
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation capella data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Capella,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionBellatrix:
		if aggDataResp.Data.Bellatrix == nil {
			gc.log.Error(clNilResponseForkDataErrMsg,
				zap.String("api", "AggregateAttestation"),
			)
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation bellatrix data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Bellatrix,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	case spec.DataVersionAltair:
		if aggDataResp.Data.Altair == nil {
			gc.log.Error(clNilResponseForkDataErrMsg,
				zap.String("api", "AggregateAttestation"),
			)
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation altair data is nil")
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       aggDataResp.Data.Altair,
			SelectionProof:  selectionProof,
		}, aggDataResp.Data.Version, nil
	default:
		if aggDataResp.Data.Phase0 == nil {
			gc.log.Error(clNilResponseForkDataErrMsg,
				zap.String("api", "AggregateAttestation"),
			)
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation phase0 data is nil")
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

// IsAggregator returns true if the signature is from the input validator. The committee
// count is provided as an argument rather than imported implementation from spec. Having
// committee count as an argument allows cheaper computation at run time.
//
// Spec pseudocode definition:
//
//	def is_aggregator(state: BeaconState, slot: Slot, index: CommitteeIndex, slot_signature: BLSSignature) -> bool:
//	 committee = get_beacon_committee(state, slot, index)
//	 modulo = max(1, len(committee) // TARGET_AGGREGATORS_PER_COMMITTEE)
//	 return bytes_to_uint64(hash(slot_signature)[0:8]) % modulo == 0
func (gc *GoClient) isAggregator(committeeCount uint64, slotSig []byte) bool {
	modulo := committeeCount / gc.beaconConfig.TargetAggregatorsPerCommittee
	if modulo == 0 {
		// Modulo must be at least 1.
		modulo = 1
	}

	b := Hash(slotSig)
	return binary.LittleEndian.Uint64(b[:8])%modulo == 0
}

// waitToSlotTwoThirds waits until two-third of the slot has transpired (SECONDS_PER_SLOT * 2 / 3 seconds after slot start time)
func (gc *GoClient) waitToSlotTwoThirds(slot phase0.Slot) {
	config := gc.getBeaconConfig()
	oneInterval := config.IntervalDuration()
	finalTime := config.GetSlotStartTime(slot).Add(2 * oneInterval)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}
	time.Sleep(wait)
}
