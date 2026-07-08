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
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
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

type gloasVoteChecker struct {
	signer          ekm.BeaconSigner
	slot            phase0.Slot
	sharePublicKeys []phase0.BLSPubKey
	expectedVote    *gloas.GloasBeaconVote
}

// NewGloasVoteChecker validates the committee runner's consensus value on Gloas-and-later slots
// (SIP #94 §2). It mirrors NewVoteChecker — slashing protection plus epoch-only majority-fork
// protection — and adds the one Gloas rule: AttestationDataIndex, the BN-supplied payload-status
// index, must be 0 or 1. That index is trusted from the QBFT leader, not compared against the
// operator's own view, exactly as the runner already trusts the leader's block root.
func NewGloasVoteChecker(
	signer ekm.BeaconSigner,
	slot phase0.Slot,
	sharePublicKeys []phase0.BLSPubKey,
	expectedVote *gloas.GloasBeaconVote,
) ValueChecker {
	return &gloasVoteChecker{
		signer:          signer,
		slot:            slot,
		sharePublicKeys: sharePublicKeys,
		expectedVote:    expectedVote,
	}
}

func (v *gloasVoteChecker) CheckValue(value []byte) error {
	bv := gloas.GloasBeaconVote{}
	if err := bv.Decode(value); err != nil {
		return spectypes.WrapError(spectypes.DecodeBeaconVoteErrorCode, fmt.Errorf("failed decoding gloas beacon vote: %w", err))
	}

	if bv.Source.Epoch >= bv.Target.Epoch {
		return spectypes.NewError(spectypes.AttestationSourceNotLessThanTargetErrorCode, "attestation data source >= target")
	}

	// SIP #94 §2: AttestationDataIndex carries the attester's payload-status view (0 = EMPTY,
	// 1 = FULL), so it must be 0 or 1. The same-slot "index = 0" rule is BN/gossip-enforced — it needs
	// the attested block's slot — so it is not checked here.
	if bv.AttestationDataIndex > 1 {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "gloas attestation data index out of range")
	}

	attestationData := &phase0.AttestationData{
		Slot: v.slot,
		// The decided payload-status index — the same value constructAttestationData will sign — so the
		// slashing pre-check sees exactly the signed data. (SSV slashing protection is epoch-only, so the
		// index doesn't change today's outcome, but keeping the two in sync is correct and future-proof.)
		Index:           bv.AttestationDataIndex,
		BeaconBlockRoot: bv.BlockRoot,
		Source:          bv.Source,
		Target:          bv.Target,
	}

	for _, sharePublicKey := range v.sharePublicKeys {
		if err := v.signer.IsAttestationSlashable(sharePublicKey, attestationData); err != nil {
			return err
		}
	}

	// Epoch-only majority-fork protection (sips/majority_fork_protection.md), as in NewVoteChecker.
	if bv.Source.Epoch != v.expectedVote.Source.Epoch {
		return fmt.Errorf("unexpected source epoch %v, expected %v", bv.Source.Epoch, v.expectedVote.Source.Epoch)
	}
	if bv.Target.Epoch != v.expectedVote.Target.Epoch {
		return fmt.Errorf("unexpected target epoch %v, expected %v", bv.Target.Epoch, v.expectedVote.Target.Epoch)
	}

	return nil
}

type envelopeChecker struct {
	proposedBlockRoots *ProposedBlockRoots
	slot               phase0.Slot
	validatorPK        spectypes.ValidatorPK
	validatorIndex     phase0.ValidatorIndex
}

// NewEnvelopeChecker validates the §6 envelope-signing duty's QBFT value (SIP #94 §6): an
// EnvelopeConsensusData carrying a self-build BlindedExecutionPayloadEnvelope whose BeaconBlockRoot
// matches the §4-decided block root for the slot (read from the store the proposer runner wrote). The
// envelope's content is leader-trusted — no PayloadRoot/field validation — matching the blinded-block
// trust model in the proposer path.
func NewEnvelopeChecker(
	proposedBlockRoots *ProposedBlockRoots,
	slot phase0.Slot,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) ValueChecker {
	return &envelopeChecker{
		proposedBlockRoots: proposedBlockRoots,
		slot:               slot,
		validatorPK:        validatorPK,
		validatorIndex:     validatorIndex,
	}
}

func (v *envelopeChecker) CheckValue(value []byte) error {
	cd := &gloas.EnvelopeConsensusData{}
	if err := cd.Decode(value); err != nil {
		return spectypes.WrapError(spectypes.QBFTValueInvalidErrorCode, fmt.Errorf("failed decoding envelope consensus data: %w", err))
	}

	if cd.Duty.Slot != v.slot {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "wrong envelope duty slot")
	}
	if cd.Duty.ValidatorIndex != v.validatorIndex {
		return spectypes.NewError(spectypes.WrongValidatorIndexErrorCode, "wrong validator index")
	}
	if !bytes.Equal(cd.Duty.PubKey[:], v.validatorPK[:]) {
		return spectypes.NewError(spectypes.WrongValidatorPubkeyErrorCode, "wrong validator pk")
	}

	blinded := &gloas.BlindedExecutionPayloadEnvelope{}
	if err := blinded.Decode(cd.DataSSZ); err != nil {
		return spectypes.WrapError(spectypes.QBFTValueInvalidErrorCode, fmt.Errorf("failed decoding blinded envelope: %w", err))
	}

	// This duty applies only to the self-build path; external builders sign their own envelopes.
	if blinded.BuilderIndex != gloas.BuilderIndexSelfBuild {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "envelope builder index is not self-build")
	}

	// The envelope must commit to the block the §4 QBFT decided for this slot.
	decidedRoot, ok := v.proposedBlockRoots.Get(v.slot)
	if !ok {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "no decided block root for envelope slot")
	}
	if blinded.BeaconBlockRoot != decidedRoot {
		return spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "envelope beacon block root does not match the decided block")
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
	cd, gloasBlock, err := checkValidatorConsensusData(value, v.beaconConfig, spectypes.BNRoleProposer, v.validatorPK, v.validatorIndex)
	if err != nil {
		return err
	}

	var slot phase0.Slot
	if gloasBlock != nil {
		// Gloas blocks have no spectypes block version; checkValidatorConsensusData already decoded the
		// node-side block and verified block.Slot == duty slot, so reuse it rather than decode again.
		slot = gloasBlock.Slot
	} else {
		blockData, _, bdErr := cd.GetBlockData()
		if bdErr != nil {
			return fmt.Errorf("could not get block data: %w", bdErr)
		}
		slot, bdErr = blockData.Slot()
		if bdErr != nil {
			return fmt.Errorf("failed to get slot from block data: %w", bdErr)
		}
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
	_, _, err := checkValidatorConsensusData(value, v.beaconConfig, spectypes.BNRoleAggregator, v.validatorPK, v.validatorIndex)
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
	_, _, err := checkValidatorConsensusData(value, v.beaconConfig, spectypes.BNRoleSyncCommitteeContribution, v.validatorPK, v.validatorIndex)
	return err
}

// checkValidatorConsensusData decodes and validates a ProposerConsensusData value. On the Gloas
// proposer path it also decodes the node-side block and returns it (nil otherwise) so callers reuse it
// instead of decoding the ~MB block a second time.
func checkValidatorConsensusData(
	value []byte,
	beaconConfig *networkconfig.Beacon,
	expectedType spectypes.BeaconRole,
	validatorPK spectypes.ValidatorPK,
	validatorIndex phase0.ValidatorIndex,
) (*spectypes.ProposerConsensusData, *gloas.BeaconBlock, error) {
	cd := &spectypes.ProposerConsensusData{}
	if err := cd.Decode(value); err != nil {
		return nil, nil, fmt.Errorf("failed decoding consensus data: %w", err)
	}

	var gloasBlock *gloas.BeaconBlock
	if cd.Duty.Type == spectypes.BNRoleProposer && beaconConfig.IsGloasAtSlot(cd.Duty.Slot) {
		// The leader-stamped Version must agree with the slot's fork. ssv-spec's ProposerValueCheckF
		// branches to Gloas on cd.Version, whereas we branch on the slot; without this guard a value on a
		// Gloas slot carrying a pre-Gloas Version would be accepted here (slot-based) but rejected there
		// (version-based), splitting the value check across a mixed cluster. Reject the mismatch so both
		// bases agree — honest proposers always stamp Version == the slot's fork. (The reverse, a Gloas
		// Version on a pre-Gloas slot, takes the else branch and is rejected by GetBlockData's
		// unknown-version error.)
		if cd.Version < networkconfig.DataVersionGloas {
			return cd, nil, spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "value version does not match slot fork")
		}
		// Gloas blocks have no spectypes block version, so ValidateConsensusData's GetBlockData path
		// can't decode them; a successful node-side decode is the validity check.
		block, err := gloas.DecodeBeaconBlock(cd.DataSSZ)
		if err != nil {
			return cd, nil, spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "invalid value")
		}
		// Pin the block's own slot to the duty slot: the block is signed under block.Slot and slashing
		// protection keys on it, so a leader that decoupled the two could harvest a signature for another
		// slot — an equivocation the slashing DB would miss. Also bounds block.Slot to the far-future check.
		if block.Slot != cd.Duty.Slot {
			return cd, nil, spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "gloas block slot does not match duty slot")
		}
		gloasBlock = block
	} else if err := ssvtypes.ValidateConsensusData(cd); err != nil {
		return cd, nil, spectypes.NewError(spectypes.QBFTValueInvalidErrorCode, "invalid value")
	}

	if expectedType != cd.Duty.Type {
		return cd, nil, spectypes.NewError(spectypes.WrongBeaconRoleTypeErrorCode, "wrong beacon role type")
	}

	if beaconConfig.EstimatedEpochAtSlot(cd.Duty.Slot) > beaconConfig.EstimatedCurrentEpoch()+1 {
		return cd, nil, spectypes.NewError(spectypes.DutyEpochTooFarFutureErrorCode, "duty epoch is into far future")
	}

	if !bytes.Equal(validatorPK[:], cd.Duty.PubKey[:]) {
		return cd, nil, spectypes.NewError(spectypes.WrongValidatorPubkeyErrorCode, "wrong validator pk")
	}

	if validatorIndex != cd.Duty.ValidatorIndex {
		return cd, nil, spectypes.NewError(spectypes.WrongValidatorIndexErrorCode, "wrong validator index")
	}

	return cd, gloasBlock, nil
}
