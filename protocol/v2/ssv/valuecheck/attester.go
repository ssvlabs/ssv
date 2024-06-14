package valuecheck

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func AttesterValueCheckF(
	signer spectypes.BeaconSigner,
	network spectypes.BeaconNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
	sharePublicKey []byte,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleAttester, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		attestationData, _ := GetAttestationData(cd) // error checked in cd.validate()

		if cd.Duty.Slot != attestationData.Slot {
			return errors.New("attestation data slot != duty slot")
		}

		if cd.Duty.CommitteeIndex != attestationData.Index {
			return errors.New("attestation data CommitteeIndex != duty CommitteeIndex")
		}

		if attestationData.Target.Epoch > network.EstimatedCurrentEpoch()+1 {
			return errors.New("attestation data target epoch is into far future")
		}

		if attestationData.Source.Epoch >= attestationData.Target.Epoch {
			return errors.New("attestation data source > target")
		}

		return signer.IsAttestationSlashable(sharePublicKey, attestationData)
	}
}

func GetAttestationData(ci *spectypes.ConsensusData) (*phase0.AttestationData, error) {
	ret := &phase0.AttestationData{}
	if err := ret.UnmarshalSSZ(ci.DataSSZ); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal ssz")
	}
	return ret, nil
}
