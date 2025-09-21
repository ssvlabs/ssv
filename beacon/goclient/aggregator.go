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

	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data: %w", err)
	}

	// Explicitly set Index field as beacon nodes may return inconsistent values.
	// EIP-7549: For Electra and later, index must always be 0, pre-Electra uses committee index.
	dataVersion, _ := gc.beaconConfig.ForkAtEpoch(gc.getBeaconConfig().EstimatedEpochAtSlot(attData.Slot))
	attData.Index = 0
	if dataVersion < spec.DataVersionElectra {
		attData.Index = committeeIndex
	}

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

	vAtt := aggDataResp.Data
	switch vAtt.Version {
	case spec.DataVersionPhase0:
		if vAtt.Phase0 == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Phase0,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	case spec.DataVersionAltair:
		if vAtt.Altair == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Altair,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	case spec.DataVersionBellatrix:
		if vAtt.Bellatrix == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Bellatrix,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	case spec.DataVersionCapella:
		if vAtt.Capella == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Capella,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	case spec.DataVersionDeneb:
		if vAtt.Deneb == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &phase0.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Deneb,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	case spec.DataVersionElectra:
		if vAtt.Electra == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Electra,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	case spec.DataVersionFulu:
		if vAtt.Fulu == nil {
			gc.log.Error(clNilResponseForkDataErrMsg, zap.String("api", "AggregateAttestation"))
			return nil, DataVersionNil, fmt.Errorf("aggregate attestation %s data is nil", vAtt.Version.String())
		}
		return &electra.AggregateAndProof{
			AggregatorIndex: index,
			Aggregate:       vAtt.Fulu,
			SelectionProof:  selectionProof,
		}, vAtt.Version, nil
	default:
		return nil, DataVersionNil, fmt.Errorf("unknown data version: %d", vAtt.Version)
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
