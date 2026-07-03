package runner

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

func (b *BaseRunner) ValidatePreConsensusMsg(
	ctx context.Context,
	runner Runner,
	psigMsgs *spectypes.PartialSignatureMessages,
) error {
	if !b.hasDutyAssigned() {
		return spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrNoDutyAssigned)
	}
	if b.hasDutySucceeded() {
		return spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrRunningDutySucceeded)
	}

	currentDutySlot, err := b.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}
	if err := b.validatePartialSigMsg(psigMsgs, currentDutySlot); err != nil {
		return err
	}

	roots, domain, err := runner.expectedPreConsensusRootsAndDomain()
	if err != nil {
		return fmt.Errorf("compute pre-consensus roots and domain: %w", err)
	}

	return b.verifyExpectedRoot(ctx, runner, psigMsgs, roots, domain)
}

// Verify each signature in container removing the invalid ones
func (b *BaseRunner) FallBackAndVerifyEachSignature(container *ssv.PartialSigContainer, root [32]byte,
	committee []*spectypes.ShareMember, validatorIndex phase0.ValidatorIndex) {
	signatures := container.GetSignatures(validatorIndex, root)

	for operatorID, signature := range signatures {
		if err := ssvtypes.VerifyBeaconPartialSignature(operatorID, signature, root, committee); err != nil {
			container.Remove(validatorIndex, operatorID, root)
		}
	}
}

func (b *BaseRunner) ValidatePostConsensusMsg(ctx context.Context, runner Runner, psigMsgs *spectypes.PartialSignatureMessages) error {
	if !b.hasDutyAssigned() {
		return spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrNoDutyAssigned)
	}
	if b.hasDutySucceeded() {
		return spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrRunningDutySucceeded)
	}

	// slotIsRelevant ensures the post-consensus message is even remotely relevant (eg. we might have already
	// moved on to another duty that's targeting the next slot but received a post-consensus message relevant
	// for the duty from the previous slot), this is a relaxed check that helps to filter out inappropriate
	// messages as soon as possible (so we can drop non-retryable messages ASAP), the exact slot validation
	// occurs below.
	currentDutySlot, err := b.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}
	slotIsRelevant := func(slot phase0.Slot) error {
		minSlot := currentDutySlot - 1
		maxSlot := currentDutySlot
		if psigMsgs.Slot < minSlot {
			// This message is targeting a slot that's already too far in the past to matter.
			return spectypes.WrapError(spectypes.PartialSigMessageInvalidSlotErrorCode, fmt.Errorf(
				"invalid partial sig slot: %d, want at least: %d",
				psigMsgs.Slot,
				minSlot,
			))
		}
		if psigMsgs.Slot > maxSlot {
			return NewRetryableError(spectypes.WrapError(spectypes.PartialSigMessageFutureSlotErrorCode, fmt.Errorf(
				"%w: message slot: %d, want at most: %d",
				ErrFuturePartialSigMsg,
				psigMsgs.Slot,
				maxSlot,
			)))
		}
		return nil
	}
	if err := slotIsRelevant(psigMsgs.Slot); err != nil {
		return err
	}

	if !b.HasStartedQBFTInstance() {
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningConsensusInstanceErrorCode, ErrInstanceNotFound))
	}

	// TODO https://github.com/ssvlabs/ssv-spec/issues/142 need to fix with this issue solution instead.
	decided, decidedValueBytes := b.State.RunningInstance.IsDecided()
	if !decided || len(b.State.DecidedValue) == 0 {
		return NewRetryableError(spectypes.WrapError(spectypes.NoDecidedValueErrorCode, ErrNoDecidedValue))
	}

	// Validate the post-consensus message differently depending on a message type.
	validateMsg := func() error {
		decidedValue := &spectypes.ProposerConsensusData{}
		if err := decidedValue.Decode(decidedValueBytes); err != nil {
			return fmt.Errorf("failed to parse decided value to ValidatorConsensusData: %w", err)
		}

		// Use the slot we have in decidedValue since b.State.CurrentDuty might have already moved on
		// to another duty (hence we shouldn't be using it).
		expectedSlot := decidedValue.Duty.Slot
		if err := b.validatePartialSigMsg(psigMsgs, expectedSlot); err != nil {
			return err
		}

		if err := b.validateValidatorIndexInPartialSigMsg(psigMsgs); err != nil {
			return err
		}

		roots, domain, err := runner.expectedPostConsensusRootsAndDomain(ctx)
		if err != nil {
			return err
		}

		return b.verifyExpectedRoot(ctx, runner, psigMsgs, roots, domain)
	}
	if runner.GetRole() == spectypes.RoleCommittee {
		validateMsg = func() error {
			decidedValue := &spectypes.BeaconVote{}
			if err := decidedValue.Decode(decidedValueBytes); err != nil {
				return fmt.Errorf("failed to parse decided value to BeaconVote: %w", err)
			}

			// Use current duty slot since CurrentDuty never changes for CommitteeRunner
			// by design, hence there is no need to store slot number on decidedValue for CommitteeRunner.
			expectedSlot, err := b.currentDutySlot()
			if err != nil {
				return fmt.Errorf("current duty slot: %w", err)
			}
			return b.validatePartialSigMsg(psigMsgs, expectedSlot)
		}
	}

	return validateMsg()
}

func (b *BaseRunner) validateDecidedConsensusData(valueCheckFn specqbft.ProposedValueCheckF, val spectypes.Encoder) error {
	byts, err := val.Encode()
	if err != nil {
		return fmt.Errorf("could not encode decided value: %w", err)
	}
	if err := valueCheckFn(byts); err != nil {
		return fmt.Errorf("decided value is invalid: %w", err)
	}

	return nil
}

func (b *BaseRunner) verifyExpectedRoot(
	ctx context.Context,
	runner Runner,
	psigMsgs *spectypes.PartialSignatureMessages,
	expectedRootObjs []ssz.HashRoot,
	domain phase0.DomainType,
) error {
	if len(expectedRootObjs) != len(psigMsgs.Messages) {
		return spectypes.NewError(spectypes.WrongRootsCountErrorCode, "wrong expected roots count")
	}

	// convert expected roots to map and mark unique roots when verified
	sortedExpectedRoots, err := func(expectedRootObjs []ssz.HashRoot) ([][32]byte, error) {
		currentDutySlot, err := b.currentDutySlot()
		if err != nil {
			return nil, fmt.Errorf("current duty slot: %w", err)
		}
		epoch := b.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot)
		d, err := runner.GetBeaconNode().DomainData(ctx, epoch, domain)
		if err != nil {
			return nil, fmt.Errorf("could not get pre consensus root domain: %w", err)
		}

		ret := make([][32]byte, 0, len(expectedRootObjs))
		for _, rootI := range expectedRootObjs {
			r, err := spectypes.ComputeETHSigningRoot(rootI, d)
			if err != nil {
				return nil, fmt.Errorf("could not compute ETH signing root: %w", err)
			}
			ret = append(ret, r)
		}

		sort.Slice(ret, func(i, j int) bool {
			return string(ret[i][:]) < string(ret[j][:])
		})
		return ret, nil
	}(expectedRootObjs)
	if err != nil {
		return err
	}

	sortedRoots := func(msgs spectypes.PartialSignatureMessages) [][32]byte {
		ret := make([][32]byte, 0, len(msgs.Messages))
		for _, msg := range msgs.Messages {
			ret = append(ret, msg.SigningRoot)
		}

		sort.Slice(ret, func(i, j int) bool {
			return string(ret[i][:]) < string(ret[j][:])
		})
		return ret
	}(*psigMsgs)

	// verify roots
	for i, r := range sortedRoots {
		if !bytes.Equal(sortedExpectedRoots[i][:], r[:]) {
			return spectypes.NewError(spectypes.WrongSigningRootErrorCode, "wrong signing root")
		}
	}
	return nil
}
