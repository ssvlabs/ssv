package ssv

import (
	"bytes"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type ValueChecker interface {
	CheckValue(value []byte) error
}

type VoteChecker interface {
	ValueChecker
	CheckValueWithSP(value []byte, spData *ssvtypes.SlashingProtectionData) error
}

type voteChecker struct {
	signer                ekm.BeaconSigner
	slot                  phase0.Slot
	sharePublicKeys       []phase0.BLSPubKey
	estimatedCurrentEpoch phase0.Epoch
}

func NewVoteChecker(
	signer ekm.BeaconSigner,
	slot phase0.Slot,
	sharePublicKeys []phase0.BLSPubKey,
	estimatedCurrentEpoch phase0.Epoch,
) VoteChecker {
	return &voteChecker{
		signer:                signer,
		slot:                  slot,
		sharePublicKeys:       sharePublicKeys,
		estimatedCurrentEpoch: estimatedCurrentEpoch,
	}
}

func (v *voteChecker) CheckValue(value []byte) error {
	_, err := v.checkValue(value)
	return err
}

func (v *voteChecker) CheckValueWithSP(value []byte, spData *ssvtypes.SlashingProtectionData) error {
	bv, err := v.checkValue(value)
	if err != nil {
		return err
	}

	// Implemented according to https://github.com/ssvlabs/SIPs/discussions/70
	if bv.Source.Epoch != spData.SourceEpoch {
		return errors.New("beacon vote source epoch doesn't satisfy slashing protection data")
	}

	if bv.Target.Epoch != spData.TargetEpoch {
		return errors.New("beacon vote target epoch doesn't satisfy slashing protection data")
	}

	return nil
}

func (v *voteChecker) checkValue(value []byte) (*spectypes.BeaconVote, error) {
	bv := spectypes.BeaconVote{}
	if err := bv.Decode(value); err != nil {
		return nil, errors.Wrap(err, "failed decoding beacon vote")
	}

	if bv.Target.Epoch > v.estimatedCurrentEpoch+1 {
		return nil, errors.New("attestation data target epoch is into far future")
	}

	if bv.Source.Epoch >= bv.Target.Epoch {
		return nil, errors.New("attestation data source >= target")
	}

	attestationData := &phase0.AttestationData{
		Slot: v.slot,
		// Consensus data is unaware of CommitteeIndex
		// We use -1 to not run into issues with the duplicate value slashing check:
		// (data_1 != data_2 and data_1.target.epoch == data_2.target.epoch)
		Index:           math.MaxUint64,
		BeaconBlockRoot: bv.BlockRoot,
		Source:          bv.Source,
		Target:          bv.Target,
	}

	for _, sharePublicKey := range v.sharePublicKeys {
		if err := v.signer.IsAttestationSlashable(sharePublicKey, attestationData); err != nil {
			return nil, err
		}
	}
	return &bv, nil
}

type proposerChecker struct {
	signer         ekm.BeaconSigner
	beaconConfig   networkconfig.Beacon
	validatorPK    spectypes.ValidatorPK
	validatorIndex phase0.ValidatorIndex
	sharePublicKey phase0.BLSPubKey
}

func NewProposerChecker(
	signer ekm.BeaconSigner,
	beaconConfig networkconfig.Beacon,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
	sharePublicKey phase0.BLSPubKey,
) ValueChecker {
	return &proposerChecker{
		signer:         signer,
		beaconConfig:   beaconConfig,
		validatorPK:    validatorPK,
		validatorIndex: validatorIndex,
		sharePublicKey: sharePublicKey,
	}
}

func (v *proposerChecker) CheckValue(value []byte) error {
	cd := &spectypes.ValidatorConsensusData{}
	if err := cd.Decode(value); err != nil {
		return errors.Wrap(err, "failed decoding consensus data")
	}
	if err := cd.Validate(); err != nil {
		return errors.Wrap(err, "invalid value")
	}

	if err := dutyValueCheck(&cd.Duty, v.beaconConfig, spectypes.BNRoleProposer, v.validatorPK, v.validatorIndex); err != nil {
		return errors.Wrap(err, "duty invalid")
	}

	if blockData, _, err := cd.GetBlindedBlockData(); err == nil {
		slot, err := blockData.Slot()
		if err != nil {
			return errors.Wrap(err, "failed to get slot from blinded block data")
		}

		return v.signer.IsBeaconBlockSlashable(v.sharePublicKey, slot)
	}
	if blockData, _, err := cd.GetBlockData(); err == nil {
		slot, err := blockData.Slot()
		if err != nil {
			return errors.Wrap(err, "failed to get slot from block data")
		}

		return v.signer.IsBeaconBlockSlashable(v.sharePublicKey, slot)
	}

	return errors.New("no block data")
}

type aggregatorChecker struct {
	beaconConfig   networkconfig.Beacon
	validatorPK    spectypes.ValidatorPK
	validatorIndex phase0.ValidatorIndex
}

func NewAggregatorChecker(
	beaconConfig networkconfig.Beacon,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) ValueChecker {
	return &aggregatorChecker{
		beaconConfig:   beaconConfig,
		validatorPK:    validatorPK,
		validatorIndex: validatorIndex,
	}
}

func (v *aggregatorChecker) CheckValue(value []byte) error {
	cd := &spectypes.ValidatorConsensusData{}
	if err := cd.Decode(value); err != nil {
		return errors.Wrap(err, "failed decoding consensus data")
	}
	if err := cd.Validate(); err != nil {
		return errors.Wrap(err, "invalid value")
	}

	if err := dutyValueCheck(&cd.Duty, v.beaconConfig, spectypes.BNRoleAggregator, v.validatorPK, v.validatorIndex); err != nil {
		return errors.Wrap(err, "duty invalid")
	}

	return nil
}

type syncCommitteeContributionChecker struct {
	beaconConfig   networkconfig.Beacon
	validatorPK    spectypes.ValidatorPK
	validatorIndex phase0.ValidatorIndex
}

func NewSyncCommitteeContributionChecker(
	beaconConfig networkconfig.Beacon,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) ValueChecker {
	return &syncCommitteeContributionChecker{
		beaconConfig:   beaconConfig,
		validatorPK:    validatorPK,
		validatorIndex: validatorIndex,
	}
}

func (v *syncCommitteeContributionChecker) CheckValue(value []byte) error {
	cd := &spectypes.ValidatorConsensusData{}
	if err := cd.Decode(value); err != nil {
		return errors.Wrap(err, "failed decoding consensus data")
	}
	if err := cd.Validate(); err != nil {
		return errors.Wrap(err, "invalid value")
	}

	if err := dutyValueCheck(&cd.Duty, v.beaconConfig, spectypes.BNRoleSyncCommitteeContribution, v.validatorPK, v.validatorIndex); err != nil {
		return errors.Wrap(err, "duty invalid")
	}

	return nil
}

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
