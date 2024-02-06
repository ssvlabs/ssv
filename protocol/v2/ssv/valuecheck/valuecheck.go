package valuecheck

import (
	"bytes"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func dutyValueCheck(
	duty *spectypes.Duty,
	network beaconprotocol.SpecNetwork,
	expectedType spectypes.BeaconRole,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) error {
	if network.EstimatedEpochAtSlot(duty.Slot) > network.EstimatedCurrentEpoch()+1 {
		return errors.New("duty epoch is into far future")
	}

	if expectedType != duty.Type {
		return errors.New("wrong beacon role type")
	}

	if !bytes.Equal(validatorPK, duty.PubKey[:]) {
		return errors.New("wrong validator pk")
	}

	if validatorIndex != duty.ValidatorIndex {
		return errors.New("wrong validator index")
	}

	return nil
}

func AttesterValueCheckF(
	signer spectypes.BeaconSigner,
	network beaconprotocol.SpecNetwork,
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

		attestationData, _ := cd.GetAttestationData() // error checked in cd.validate()

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

func ProposerValueCheckF(
	signer spectypes.BeaconSigner,
	network beaconprotocol.SpecNetwork,
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

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleProposer, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		if blockData, _, err := cd.GetBlindedBlockData(); err == nil {
			slot, err := blockData.Slot()
			if err != nil {
				return errors.Wrap(err, "failed to get slot from blinded block data")
			}
			return signer.IsBeaconBlockSlashable(sharePublicKey, slot)
		}
		if blockData, _, err := cd.GetBlockData(); err == nil {
			slot, err := blockData.Slot()
			if err != nil {
				return errors.Wrap(err, "failed to get slot from block data")
			}
			return signer.IsBeaconBlockSlashable(sharePublicKey, slot)
		}

		return errors.New("no block data")
	}
}

func AggregatorValueCheckF(
	signer spectypes.BeaconSigner,
	network beaconprotocol.SpecNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleAggregator, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}
		return nil
	}
}

func SyncCommitteeValueCheckF(
	signer spectypes.BeaconSigner,
	network beaconprotocol.SpecNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleSyncCommittee, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}
		return nil
	}
}

func SyncCommitteeContributionValueCheckF(
	signer spectypes.BeaconSigner,
	network beaconprotocol.SpecNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleSyncCommitteeContribution, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		//contributions, _ := cd.GetSyncCommitteeContributions()
		//
		//for _, c := range contributions {
		//	// TODO check we have selection proof for contribution
		//	// TODO check slot == duty slot
		//	// TODO check beacon block root somehow? maybe all beacon block roots should be equal?
		//
		//}
		return nil
	}
}
