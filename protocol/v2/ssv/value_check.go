package ssv

import (
	"bytes"
	"fmt"
	"math"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type ValueChecker interface {
	CheckValue(value []byte) error
}

type voteChecker struct {
	signer          ekm.BeaconSigner
	slot            phase0.Slot
	sharePublicKeys []phase0.BLSPubKey
	expectedVote    *spectypes.BeaconVote
}

func NewVoteChecker(
	signer ekm.BeaconSigner,
	slot phase0.Slot,
	sharePublicKeys []phase0.BLSPubKey,
	expectedVote *spectypes.BeaconVote,
) ValueChecker {
	return &voteChecker{
		signer:          signer,
		slot:            slot,
		sharePublicKeys: sharePublicKeys,
		expectedVote:    expectedVote,
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

	// Implemented according to sips/majority_fork_protection.md: compare epochs only.
	if bv.Source.Epoch != v.expectedVote.Source.Epoch {
		return fmt.Errorf("unexpected source epoch %v, expected %v", bv.Source.Epoch, v.expectedVote.Source.Epoch)
	}

	if bv.Target.Epoch != v.expectedVote.Target.Epoch {
		return fmt.Errorf("unexpected target epoch %v, expected %v", bv.Target.Epoch, v.expectedVote.Target.Epoch)
	}

	return nil
}

type aggregatorCommitteeChecker struct{}

func NewAggregatorCommitteeChecker() ValueChecker {
	return &aggregatorCommitteeChecker{}
}

func (v *aggregatorCommitteeChecker) CheckValue(value []byte) error {
	cd := &spectypes.AggregatorCommitteeConsensusData{}
	if err := cd.Decode(value); err != nil {
		return spectypes.WrapError(
			spectypes.AggCommConsensusDataDecodeErrorCode,
			fmt.Errorf("failed decoding aggregator committee consensus data: %w", err),
		)
	}
	if err := cd.Validate(); err != nil {
		return fmt.Errorf("invalid value: %w", err)
	}

	// spec Validate() checks that the committee/subnet index sets line up, but does not forbid a
	// validator index repeating in Aggregators or Contributors. Post-consensus emits one partial
	// signature per entry (GetAggregateAndProofs / GetSyncCommitteeContributions are one-per-entry),
	// so a duplicated entry means multiple partial signatures for the same validator index. Message
	// validation caps that at 5 per index and rejects the whole message beyond it — so a Byzantine
	// proposer could otherwise make every honest operator broadcast a message its peers reject,
	// draining their gossip score network-wide. Reject the duplicate here so honest operators never
	// sign it; an honest proposer's value is always duplicate-free (the runner dedups on build).
	if err := validateNoDuplicateAggregatorCommittee(cd); err != nil {
		return err
	}

	return nil
}

// validateNoDuplicateAggregatorCommittee rejects consensus data that assigns the same validator
// index to the same committee/subnet index more than once within a set. Aggregators and Contributors
// are deduped independently: an aggregator's CommitteeIndex (attestation committee) and a
// contributor's CommitteeIndex (sync subnet) are different namespaces, so the same (validator,
// index) pair legitimately appearing once in each set is not a duplicate.
func validateNoDuplicateAggregatorCommittee(cd *spectypes.AggregatorCommitteeConsensusData) error {
	type assignment struct {
		validatorIndex phase0.ValidatorIndex
		committeeIndex uint64
	}

	dedup := func(kind string, count int, at func(int) (phase0.ValidatorIndex, uint64)) error {
		seen := make(map[assignment]struct{}, count)
		for i := 0; i < count; i++ {
			validatorIndex, committeeIndex := at(i)
			key := assignment{validatorIndex: validatorIndex, committeeIndex: committeeIndex}
			if _, ok := seen[key]; ok {
				return fmt.Errorf("duplicate %s for validator index %d, committee index %d", kind, validatorIndex, committeeIndex)
			}
			seen[key] = struct{}{}
		}
		return nil
	}

	if err := dedup("aggregator", len(cd.Aggregators), func(i int) (phase0.ValidatorIndex, uint64) {
		return cd.Aggregators[i].ValidatorIndex, cd.Aggregators[i].CommitteeIndex
	}); err != nil {
		return err
	}
	return dedup("contributor", len(cd.Contributors), func(i int) (phase0.ValidatorIndex, uint64) {
		return cd.Contributors[i].ValidatorIndex, cd.Contributors[i].CommitteeIndex
	})
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
) (*spectypes.ProposerConsensusData, error) {
	cd := &spectypes.ProposerConsensusData{}
	if err := cd.Decode(value); err != nil {
		return nil, fmt.Errorf("failed decoding consensus data: %w", err)
	}
	if err := ssvtypes.ValidateConsensusData(cd); err != nil {
		return cd, spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "invalid value")
	}

	if expectedType != cd.Duty.Type {
		return cd, spectypes.NewError(spectypes.WrongBeaconRoleTypeErrorCode, "wrong beacon role type")
	}

	if beaconConfig.EstimatedEpochAtSlot(cd.Duty.Slot) > beaconConfig.EstimatedCurrentEpoch()+1 {
		return cd, spectypes.NewError(spectypes.DutyEpochTooFarFutureErrorCode, "duty epoch is into far future")
	}

	if !bytes.Equal(validatorPK[:], cd.Duty.PubKey[:]) {
		return cd, spectypes.NewError(spectypes.WrongValidatorPubkeyErrorCode, "wrong validator pk")
	}

	if validatorIndex != cd.Duty.ValidatorIndex {
		return cd, spectypes.NewError(spectypes.WrongValidatorIndexErrorCode, "wrong validator index")
	}

	return cd, nil
}
