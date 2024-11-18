package goclient

import (
	"context"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/observability"
	"go.opentelemetry.io/otel/metric"
)

// AttesterDuties returns attester duties for a given epoch.
func (gc *GoClient) AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error) {
	resp, err := gc.client.AttesterDuties(ctx, &api.AttesterDutiesOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain attester duties: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("attester duties response is nil")
	}

	return resp.Data, nil
}

func (gc *GoClient) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (*phase0.AttestationData, spec.DataVersion, error) {
	attDataReqStart := time.Now()
	resp, err := gc.client.AttestationData(gc.ctx, &api.AttestationDataOpts{
		Slot:           slot,
		CommitteeIndex: committeeIndex,
	})
	if err != nil {
		return nil, DataVersionNil, fmt.Errorf("failed to get attestation data: %w", err)
	}
	if resp == nil {
		return nil, DataVersionNil, fmt.Errorf("attestation data response is nil")
	}

	attestationDataRequestHistogram.Record(
		gc.ctx,
		time.Since(attDataReqStart).Seconds(),
		metric.WithAttributes(observability.BeaconRoleAttribute(spectypes.BNRoleAttester)))

	return resp.Data, spec.DataVersionPhase0, nil
}

// SubmitAttestations implements Beacon interface
func (gc *GoClient) SubmitAttestations(attestations []*phase0.Attestation) error {
	return gc.client.SubmitAttestations(gc.ctx, attestations)
}
