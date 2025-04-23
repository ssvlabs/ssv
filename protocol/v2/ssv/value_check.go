package ssv

import (
	"bytes"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

func dutyValueCheck(
	duty *spectypes.ValidatorDuty,
	network spectypes.BeaconNetwork,
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

	if !bytes.Equal(validatorPK[:], duty.PubKey[:]) {
		return errors.New("wrong validator pk")
	}

	if validatorIndex != duty.ValidatorIndex {
		return errors.New("wrong validator index")
	}

	return nil
}

func BeaconVoteValueCheckF(
	signer ekm.BeaconSigner,
	slot phase0.Slot,
	sharePublicKeys []phase0.BLSPubKey,
	estimatedCurrentEpoch phase0.Epoch,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		bv := spectypes.BeaconVote{}
		if err := bv.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding beacon vote")
		}

		if bv.Target.Epoch > estimatedCurrentEpoch+1 {
			return errors.New("attestation data target epoch is into far future")
		}

		if bv.Source.Epoch >= bv.Target.Epoch {
			return errors.New("attestation data source >= target")
		}

		attestationData := &phase0.AttestationData{
			Slot: slot,
			// Consensus data is unaware of CommitteeIndex
			// We use -1 to not run into issues with the duplicate value slashing check:
			// (data_1 != data_2 and data_1.target.epoch == data_2.target.epoch)
			Index:           math.MaxUint64,
			BeaconBlockRoot: bv.BlockRoot,
			Source:          bv.Source,
			Target:          bv.Target,
		}

		for _, sharePublicKey := range sharePublicKeys {
			if err := signer.IsAttestationSlashable(sharePublicKey, attestationData); err != nil {
				return err
			}
		}
		return nil
	}
}

func ProposerValueCheckF(
	signer ekm.BeaconSigner,
	network spectypes.BeaconNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
	sharePublicKey phase0.BLSPubKey,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ValidatorConsensusData{}
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
	signer ekm.BeaconSigner,
	network spectypes.BeaconNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ValidatorConsensusData{}
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

func SyncCommitteeContributionValueCheckF(
	signer ekm.BeaconSigner,
	network spectypes.BeaconNetwork,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) specqbft.ProposedValueCheckF {
	return func(data []byte) error {
		cd := &spectypes.ValidatorConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if err := dutyValueCheck(&cd.Duty, network, spectypes.BNRoleSyncCommitteeContribution, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		return nil
	}
}
