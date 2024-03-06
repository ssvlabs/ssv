package valuechecker

import (
	"fmt"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/types"
)

func (vc *ValueChecker) AttesterValueCheckF(data []byte) error {
	cd := &types.ConsensusData{}
	if err := cd.Decode(data); err != nil {
		return fmt.Errorf("decode consensus data: %w", err)
	}

	if err := cd.Validate(); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	if err := vc.checkDuty(&cd.Duty, types.BNRoleAttester); err != nil {
		return fmt.Errorf("invalid duty: %w", err)
	}

	attestationData, _ := cd.GetAttestationData() // error checked in cd.validate()

	if cd.Duty.Slot != attestationData.Slot {
		return fmt.Errorf("attestation data slot != duty slot")
	}

	if cd.Duty.CommitteeIndex != attestationData.Index {
		return fmt.Errorf("attestation data CommitteeIndex != duty CommitteeIndex")
	}

	if attestationData.Target.Epoch > vc.network.EstimatedCurrentEpoch()+1 {
		return fmt.Errorf("attestation data target epoch is into far future")
	}

	if attestationData.Source.Epoch >= attestationData.Target.Epoch {
		return fmt.Errorf("attestation data source >= target")
	}

	return vc.checkSlashableAttestation(attestationData)
}

func (vc *ValueChecker) checkSlashableAttestation(attestationData *spec.AttestationData) error {
	if equalAttestationData(vc.currentAttestationData, attestationData) {
		return nil
	}

	if err := vc.signer.IsAttestationSlashable(vc.sharePublicKey, attestationData); err != nil {
		return err
	}

	vc.currentAttestationData = attestationData
	return nil
}

func equalAttestationData(a, b *spec.AttestationData) bool {
	return a != nil && b != nil &&
		a.Slot == b.Slot &&
		a.Index == b.Index &&
		a.BeaconBlockRoot == b.BeaconBlockRoot &&
		equalCheckpoint(a.Source, b.Source) &&
		equalCheckpoint(a.Target, b.Target)
}

func equalCheckpoint(a, b *spec.Checkpoint) bool {
	return a != nil && b != nil &&
		a.Epoch == b.Epoch &&
		a.Root == b.Root
}
