package ssv

import (
	"bytes"
	"fmt"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"

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
		return errors.Wrap(err, "failed decoding beacon vote")
	}

	if bv.Target.Epoch > v.estimatedCurrentEpoch+1 {
		return errors.New("attestation data target epoch is into far future")
	}

	if bv.Source.Epoch >= bv.Target.Epoch {
		return errors.New("attestation data source >= target")
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

	if bv.Target.Root != v.expectedVote.Target.Root {
		return errors.New("beacon vote target root doesn't satisfy slashing protection data")
	}

	// Implemented according to https://github.com/ssvlabs/SIPs/discussions/70
	if bv.Source.Epoch != v.expectedVote.Source.Epoch {
		return errors.New("beacon vote source epoch doesn't satisfy slashing protection data")
	}

	if bv.Target.Epoch != v.expectedVote.Target.Epoch {
		return errors.New("beacon vote target epoch doesn't satisfy slashing protection data")
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
		return errors.Wrap(err, "could not get block data")
	}

	slot, err := blockData.Slot()
	if err != nil {
		return errors.Wrap(err, "failed to get slot from block data")
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
		return cd, fmt.Errorf("invalid value: %w", err)
	}

	if beaconConfig.EstimatedEpochAtSlot(cd.Duty.Slot) > beaconConfig.EstimatedCurrentEpoch()+1 {
		return cd, fmt.Errorf("duty epoch is in the far future")
	}

	if expectedType != cd.Duty.Type {
		return cd, fmt.Errorf("wrong beacon role type")
	}

	if !bytes.Equal(validatorPK[:], cd.Duty.PubKey[:]) {
		return cd, fmt.Errorf("wrong validator pk")
	}

	if validatorIndex != cd.Duty.ValidatorIndex {
		return cd, fmt.Errorf("wrong validator index")
	}

	return cd, nil
}

type noOpValueChecker struct{}

func NewNoOpValueChecker() ValueChecker {
	return &noOpValueChecker{}
}

func (*noOpValueChecker) CheckValue(value []byte) error { return nil }
