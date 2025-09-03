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
	if err := gc.waitTwoThirdsIntoSlot(ctx, slot); err != nil {
		return nil, 0, fmt.Errorf("wait for 2/3 of slot: %w", err)
	}

	attData, _, err := gc.GetAttestationData(ctx, slot)
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data: %w", err)
	}

	dataVersion, _ := gc.beaconConfig.ForkAtEpoch(gc.getBeaconConfig().EstimatedEpochAtSlot(attData.Slot))
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

// waitTwoThirdsIntoSlot waits until two-third of the slot has transpired (SECONDS_PER_SLOT * 2 / 3 seconds after the start of slot)
func (gc *GoClient) waitTwoThirdsIntoSlot(ctx context.Context, slot phase0.Slot) error {
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
