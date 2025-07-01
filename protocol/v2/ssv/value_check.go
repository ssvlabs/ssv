package ssv

import (
	"bytes"
	"fmt"
	"math"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type ProposedValueCheckF func(data, ownData []byte) error

func dutyValueCheck(
	duty *spectypes.ValidatorDuty,
	beaconConfig networkconfig.Beacon,
	expectedType spectypes.BeaconRole,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) error {
	if beaconConfig.EstimatedEpochAtSlot(duty.Slot) > beaconConfig.EstimatedCurrentEpoch()+1 {
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
) ProposedValueCheckF {
	// TODO: consider passing ownData unmarshaled to avoid redundant encoding/decoding
	return func(data, ownData []byte) error {
		bv := spectypes.BeaconVote{}
		if err := bv.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding beacon vote")
		}

		if ownData != nil {
			obv := spectypes.BeaconVote{}
			if err := obv.Decode(ownData); err != nil {
				return errors.Wrap(err, "failed decoding own beacon vote")
			}

			if bv.BlockRoot != obv.BlockRoot {
				return errors.New("beacon vote block root differs from own beacon vote")
			}

			if bv.Source == nil && obv.Source != nil || bv.Source != nil && obv.Source == nil || *bv.Source != *obv.Source {
				return errors.New("beacon vote source differs from own beacon vote")
			}

			if bv.Target == nil && obv.Target != nil || bv.Target != nil && obv.Target == nil || *bv.Target != *obv.Target {
				return errors.New("beacon vote target differs from own beacon vote")
			}
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
	beaconConfig networkconfig.Beacon,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
	sharePublicKey phase0.BLSPubKey,
) ProposedValueCheckF {
	// TODO: consider passing ownData unmarshaled to avoid redundant encoding/decoding
	return func(data, ownData []byte) error {
		cd := &spectypes.ValidatorConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if ownData != nil {
			ocd := &spectypes.ValidatorConsensusData{}
			if err := ocd.Decode(ownData); err != nil {
				return errors.Wrap(err, "failed decoding own consensus data")
			}
			if err := ocd.Validate(); err != nil {
				// TODO: should we panic?
				return errors.Wrap(err, "invalid own value")
			}

			if err := checkConsensusDataSameToOwn(cd, ocd); err != nil {
				return fmt.Errorf("check own blinded block data: %w", err)
			}
		}

		if err := dutyValueCheck(&cd.Duty, beaconConfig, spectypes.BNRoleProposer, validatorPK, validatorIndex); err != nil {
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
	beaconConfig networkconfig.Beacon,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) ProposedValueCheckF {
	// TODO: consider passing ownData unmarshaled to avoid redundant encoding/decoding
	return func(data, ownData []byte) error {
		cd := &spectypes.ValidatorConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if ownData != nil {
			ocd := &spectypes.ValidatorConsensusData{}
			if err := ocd.Decode(ownData); err != nil {
				return errors.Wrap(err, "failed decoding own consensus data")
			}
			if err := ocd.Validate(); err != nil {
				return errors.Wrap(err, "invalid own value")
			}

			if err := checkConsensusDataSameToOwn(cd, ocd); err != nil {
				return fmt.Errorf("check own blinded block data: %w", err)
			}
		}

		if err := dutyValueCheck(&cd.Duty, beaconConfig, spectypes.BNRoleAggregator, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		return nil
	}
}

func SyncCommitteeContributionValueCheckF(
	signer ekm.BeaconSigner,
	beaconConfig networkconfig.Beacon,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) ProposedValueCheckF {
	// TODO: consider passing ownData unmarshaled to avoid redundant encoding/decoding
	return func(data, ownData []byte) error {
		cd := &spectypes.ValidatorConsensusData{}
		if err := cd.Decode(data); err != nil {
			return errors.Wrap(err, "failed decoding consensus data")
		}
		if err := cd.Validate(); err != nil {
			return errors.Wrap(err, "invalid value")
		}

		if ownData != nil {
			ocd := &spectypes.ValidatorConsensusData{}
			if err := ocd.Decode(ownData); err != nil {
				return errors.Wrap(err, "failed decoding own consensus data")
			}
			if err := ocd.Validate(); err != nil {
				return errors.Wrap(err, "invalid own value")
			}

			if err := checkConsensusDataSameToOwn(cd, ocd); err != nil {
				return fmt.Errorf("check own blinded block data: %w", err)
			}
		}

		if err := dutyValueCheck(&cd.Duty, beaconConfig, spectypes.BNRoleSyncCommitteeContribution, validatorPK, validatorIndex); err != nil {
			return errors.Wrap(err, "duty invalid")
		}

		return nil
	}
}

func checkConsensusDataSameToOwn(consensusData, ownConsensusData *spectypes.ValidatorConsensusData) error {
	if consensusData.Duty.Type != ownConsensusData.Duty.Type {
		return fmt.Errorf("validator duty Type differs from own validator duty")
	}
	if consensusData.Duty.PubKey != ownConsensusData.Duty.PubKey {
		return fmt.Errorf("validator duty PubKey differs from own validator duty")
	}
	if consensusData.Duty.Slot != ownConsensusData.Duty.Slot {
		return fmt.Errorf("validator duty Slot differs from own validator duty")
	}
	if consensusData.Duty.ValidatorIndex != ownConsensusData.Duty.ValidatorIndex {
		return fmt.Errorf("validator duty ValidatorIndex differs from own validator duty")
	}
	if consensusData.Duty.CommitteeIndex != ownConsensusData.Duty.CommitteeIndex {
		return fmt.Errorf("validator duty CommitteeIndex differs from own validator duty")
	}
	if consensusData.Duty.CommitteeLength != ownConsensusData.Duty.CommitteeLength {
		return fmt.Errorf("validator duty CommitteeLength differs from own validator duty")
	}
	if consensusData.Duty.CommitteesAtSlot != ownConsensusData.Duty.CommitteesAtSlot {
		return fmt.Errorf("validator duty CommitteesAtSlot differs from own validator duty")
	}
	if consensusData.Duty.ValidatorCommitteeIndex != ownConsensusData.Duty.ValidatorCommitteeIndex {
		return fmt.Errorf("validator duty ValidatorCommitteeIndex differs from own validator duty")
	}

	validatorSyncCommitteeIndices := make([]uint64, 13)
	copy(validatorSyncCommitteeIndices[:], consensusData.Duty.ValidatorSyncCommitteeIndices[:])

	ownValidatorSyncCommitteeIndices := make([]uint64, 13)
	copy(ownValidatorSyncCommitteeIndices[:], consensusData.Duty.ValidatorSyncCommitteeIndices[:])

	slices.Sort(validatorSyncCommitteeIndices)
	slices.Sort(ownValidatorSyncCommitteeIndices)

	if slices.Compare(validatorSyncCommitteeIndices, ownValidatorSyncCommitteeIndices) != 0 {
		return fmt.Errorf("validator duty ValidatorSyncCommitteeIndices differs from own validator duty")
	}

	if consensusData.Version != ownConsensusData.Version {
		return errors.New("validator consensus data Version differs from own data")
	}

	var blockSlot phase0.Slot
	var blockProposerIndex phase0.ValidatorIndex

	if blindedBlockData, _, err := consensusData.GetBlindedBlockData(); err == nil {
		slot, err := blindedBlockData.Slot()
		if err != nil {
			return errors.Wrap(err, "failed to get slot from blinded block data")
		}

		blockSlot = slot

		proposerIndex, err := blindedBlockData.ProposerIndex()
		if err != nil {
			return errors.Wrap(err, "failed to get block proposer index")
		}

		blockProposerIndex = proposerIndex
	} else if blockData, _, err := consensusData.GetBlockData(); err == nil {
		slot, err := blockData.Slot()
		if err != nil {
			return errors.Wrap(err, "failed to get slot from block data")
		}

		blockSlot = slot

		proposerIndex, err := blockData.ProposerIndex()
		if err != nil {
			return errors.Wrap(err, "failed to get block proposer index")
		}

		blockProposerIndex = proposerIndex
	} else {
		return fmt.Errorf("data is neither block nor blinded block")
	}

	var ownBlockSlot phase0.Slot
	var ownBlockProposerIndex phase0.ValidatorIndex

	if ownBlindedBlockData, _, err := ownConsensusData.GetBlindedBlockData(); err == nil {
		slot, err := ownBlindedBlockData.Slot()
		if err != nil {
			return errors.Wrap(err, "failed to get slot from own blinded block data")
		}

		ownBlockSlot = slot

		proposerIndex, err := ownBlindedBlockData.ProposerIndex()
		if err != nil {
			return errors.Wrap(err, "failed to get block proposer index")
		}

		ownBlockProposerIndex = proposerIndex
	} else if ownBlockData, _, err := ownConsensusData.GetBlockData(); err == nil {
		slot, err := ownBlockData.Slot()
		if err != nil {
			return errors.Wrap(err, "failed to get slot from own block data")
		}

		ownBlockSlot = slot

		proposerIndex, err := ownBlockData.ProposerIndex()
		if err != nil {
			return errors.Wrap(err, "failed to get own block proposer index")
		}

		ownBlockProposerIndex = proposerIndex
	} else {
		return fmt.Errorf("own data is neither block nor blinded block")
	}

	if blockSlot != ownBlockSlot {
		return fmt.Errorf("validator duty block slot differs from own validator duty")
	}

	if blockProposerIndex != ownBlockProposerIndex {
		return fmt.Errorf("validator duty block proposer index differs from own validator duty")
	}

	// TODO: check other fields

	return fmt.Errorf("own consensus data is neither block nor blinded block")
}
