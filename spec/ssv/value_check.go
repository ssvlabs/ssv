package ssv

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func dutyValueCheck(duty *types.Duty, network BeaconNetwork) error {
	if network.EstimatedEpochAtSlot(duty.Slot) > network.EstimatedCurrentEpoch()+1 {
		return errors.New("duty epoch is into far future")
	}
	return nil
}

func BeaconAttestationValueCheck(signer types.BeaconSigner, network BeaconNetwork) qbft.ProposedValueCheck {
	return func(data []byte) error {
		cd := &types.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}

		if err := dutyValueCheck(cd.Duty, network); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		if cd.Duty.Type != types.BNRoleAttester {
			return errors.New("duty type != RoleTypeAttester")
		}

		if cd.AttestationData == nil {
			return errors.New("attestation data nil")
		}

		if cd.Duty.Slot != cd.AttestationData.Slot {
			return errors.New("attestation data slot != duty slot")
		}

		if cd.Duty.CommitteeIndex != cd.AttestationData.Index {
			return errors.New("attestation data CommitteeIndex != duty CommitteeIndex")
		}

		// no need to test far future attestation as we check duty slot not far future && duty slot == attestation slot

		if cd.AttestationData.Target.Epoch > network.EstimatedCurrentEpoch()+1 {
			return errors.New("attestation data target epoch is into far future")
		}

		if cd.AttestationData.Source.Epoch >= cd.AttestationData.Target.Epoch {
			return errors.New("attestation data source and target epochs invalid")
		}

		return signer.IsAttestationSlashable(cd.AttestationData)
	}
}

func BeaconBlockValueCheck(signer types.BeaconSigner, network BeaconNetwork) qbft.ProposedValueCheck {
	return func(data []byte) error {
		return nil
	}
}

func AggregatorValueCheck(signer types.BeaconSigner, network BeaconNetwork) qbft.ProposedValueCheck {
	return func(data []byte) error {
		return nil
	}
}

func SyncCommitteeValueCheck(signer types.BeaconSigner, network BeaconNetwork) qbft.ProposedValueCheck {
	return func(data []byte) error {
		return nil
	}
}

func SyncCommitteeContributionValueCheck(signer types.BeaconSigner, network BeaconNetwork) qbft.ProposedValueCheck {
	return func(data []byte) error {
		cd := &types.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}

		if err := dutyValueCheck(cd.Duty, network); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		for _, c := range cd.SyncCommitteeContribution {
			if c.Slot == 0 {
				// TODO - can remove
			}

			// TODO check we have selection proof for contribution
			// TODO check slot == duty slot
			// TODO check beacon block root somehow? maybe all beacon block roots should be equal?

		}
		return nil
	}
}
