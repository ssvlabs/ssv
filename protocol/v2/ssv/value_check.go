package ssv

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
)

type ValueChecker interface {
	CheckValue(value []byte) error
}

type voteChecker struct {
	signer                ekm.BeaconSigner
	slot                  phase0.Slot
	sharePublicKeys       []phase0.BLSPubKey
	estimatedCurrentEpoch phase0.Epoch
	expectedVote          *spectypes.BeaconVote
}

func NewVoteChecker(
	signer ekm.BeaconSigner,
	slot phase0.Slot,
	sharePublicKeys []phase0.BLSPubKey,
	estimatedCurrentEpoch phase0.Epoch,
	expectedVote *spectypes.BeaconVote,
) ValueChecker {
	return &voteChecker{
		signer:                signer,
		slot:                  slot,
		sharePublicKeys:       sharePublicKeys,
		estimatedCurrentEpoch: estimatedCurrentEpoch,
		expectedVote:          expectedVote,
	}
}

func (v *voteChecker) CheckValue(value []byte) error {
	bv := spectypes.BeaconVote{}
	if err := bv.Decode(value); err != nil {
		return spectypes.WrapError(spectypes.DecodeBeaconVoteErrorCode, fmt.Errorf("failed decoding beacon vote: %w", err))
	}

	if bv.Source.Epoch >= bv.Target.Epoch {
		return spectypes.NewError(spectypes.AttestationSourceNotLessThanTargetErrorCode, "attestation data source >= target")
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
			return err
		}
	}

	// Implemented according to https://github.com/ssvlabs/SIPs/pull/69
	if bv.Source.Epoch != v.expectedVote.Source.Epoch {
		return fmt.Errorf("unexpected source epoch %v, expected %v", bv.Source.Epoch, v.expectedVote.Source.Epoch)
	}

	if bv.Target.Epoch != v.expectedVote.Target.Epoch {
		return fmt.Errorf("unexpected target epoch %v, expected %v", bv.Target.Epoch, v.expectedVote.Target.Epoch)
	}

	if bv.Source.Root != v.expectedVote.Source.Root {
		return fmt.Errorf("unexpected source root %x, expected %x", bv.Source.Root, v.expectedVote.Source.Root)
	}

	if bv.Target.Root != v.expectedVote.Target.Root {
		return fmt.Errorf("unexpected target root %x, expected %x", bv.Target.Root, v.expectedVote.Target.Root)
	}

	return nil
}

type validatorConsensusDataChecker struct{}

func NewValidatorConsensusDataChecker() ValueChecker {
	return &validatorConsensusDataChecker{}
}

func (v *validatorConsensusDataChecker) CheckValue(value []byte) error {
	cd := &spectypes.AggregatorCommitteeConsensusData{}
	if err := cd.Decode(value); err != nil {
		return fmt.Errorf("failed decoding aggregator committee consensus data: %w", err)
	}
	if err := cd.Validate(); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	// Basic validation - consensus data should have either aggregator or sync committee data
	hasAggregators := len(cd.Aggregators) > 0
	hasContributors := len(cd.Contributors) > 0

	if !hasAggregators && !hasContributors {
		return errors.New("no aggregators or sync committee contributors in consensus data")
	}

	return nil
}

type proposerChecker struct {
	signer         ekm.BeaconSigner
	beaconConfig   *networkconfig.Beacon
	validatorPK    spectypes.ValidatorPK
	validatorIndex phase0.ValidatorIndex
	sharePublicKey phase0.BLSPubKey
}

func NewProposerChecker(
	signer ekm.BeaconSigner,
	beaconConfig *networkconfig.Beacon,
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
	cd, err := checkValidatorConsensusData(value, v.beaconConfig, spectypes.BNRoleProposer, v.validatorPK, v.validatorIndex)
	if err != nil {
		return err
	}

	blockData, _, err := cd.GetBlockData()
	if err != nil {
		return fmt.Errorf("could not get block data: %w", err)
	}

	slot, err := blockData.Slot()
	if err != nil {
		return fmt.Errorf("failed to get slot from block data: %w", err)
	}
	return v.signer.IsBeaconBlockSlashable(v.sharePublicKey, slot)
}

type aggregatorChecker struct {
	beaconConfig   *networkconfig.Beacon
	validatorPK    spectypes.ValidatorPK
	validatorIndex phase0.ValidatorIndex
}

func NewAggregatorChecker(
	beaconConfig *networkconfig.Beacon,
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
	_, err := checkValidatorConsensusData(value, v.beaconConfig, spectypes.BNRoleAggregator, v.validatorPK, v.validatorIndex)
	return err
}

type syncCommitteeContributionChecker struct {
	beaconConfig   *networkconfig.Beacon
	validatorPK    spectypes.ValidatorPK
	validatorIndex phase0.ValidatorIndex
}

func NewSyncCommitteeContributionChecker(
	beaconConfig *networkconfig.Beacon,
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
	_, err := checkValidatorConsensusData(value, v.beaconConfig, spectypes.BNRoleSyncCommitteeContribution, v.validatorPK, v.validatorIndex)
	return err
}

func checkValidatorConsensusData(
	value []byte,
	beaconConfig *networkconfig.Beacon,
	expectedType spectypes.BeaconRole,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) (*spectypes.ValidatorConsensusData, error) {
	cd := &spectypes.ValidatorConsensusData{}
	if err := cd.Decode(value); err != nil {
		return nil, fmt.Errorf("failed decoding consensus data: %w", err)
	}
	if err := cd.Validate(); err != nil {
		return cd, spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "invalid value")
	}

	if beaconConfig.EstimatedEpochAtSlot(cd.Duty.Slot) > beaconConfig.EstimatedCurrentEpoch()+1 {
		return cd, spectypes.NewError(spectypes.DutyEpochTooFarFutureErrorCode, "duty epoch is into far future")
	}

	if expectedType != cd.Duty.Type {
		return cd, spectypes.NewError(spectypes.WrongBeaconRoleTypeErrorCode, "wrong beacon role type")
	}

	if !bytes.Equal(validatorPK[:], cd.Duty.PubKey[:]) {
		return cd, spectypes.NewError(spectypes.WrongValidatorPubkeyErrorCode, "wrong validator pk")
	}

	if validatorIndex != cd.Duty.ValidatorIndex {
		return cd, spectypes.NewError(spectypes.WrongValidatorIndexErrorCode, "wrong validator index")
	}

	return cd, nil
}
