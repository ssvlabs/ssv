package runner

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv"
)

func (b *BaseRunner) ValidatePreConsensusMsg(
	ctx context.Context,
	runner Runner,
	psigMsgs *spectypes.PartialSignatureMessages,
) error {
	if !b.hasRunningDuty() {
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrNoRunningDuty))
	}

	if err := b.validatePartialSigMsg(psigMsgs, b.State.CurrentDuty.DutySlot()); err != nil {
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
		if err := b.verifyBeaconPartialSignature(operatorID, signature, root, committee); err != nil {
			container.Remove(validatorIndex, operatorID, root)
		}
	}
}

func (b *BaseRunner) ValidatePostConsensusMsg(ctx context.Context, runner Runner, psigMsgs *spectypes.PartialSignatureMessages) error {
	if !b.hasRunningDuty() {
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningDutyErrorCode, ErrNoRunningDuty))
	}

	// slotIsRelevant ensures the post-consensus message is even remotely relevant (eg. we might have already
	// moved on to another duty that's targeting the next slot but received a post-consensus message relevant
	// for the duty from the previous slot), this is a relaxed check that helps to filter out inappropriate
	// messages as soon as possible (so we can drop non-retryable messages ASAP), the exact slot validation
	// occurs below.
	slotIsRelevant := func(slot phase0.Slot) error {
		minSlot := b.State.CurrentDuty.DutySlot() - 1
		maxSlot := b.State.CurrentDuty.DutySlot()
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
				"%v: message slot: %d, want at most: %d",
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

	if b.State.RunningInstance == nil {
		return NewRetryableError(spectypes.WrapError(spectypes.NoRunningConsensusInstanceErrorCode, ErrInstanceNotFound))
	}

	// TODO https://github.com/ssvlabs/ssv-spec/issues/142 need to fix with this issue solution instead.
	decided, decidedValueBytes := b.State.RunningInstance.IsDecided()
	if !decided || len(b.State.DecidedValue) == 0 {
		return NewRetryableError(spectypes.WrapError(spectypes.NoDecidedValueErrorCode, ErrNoDecidedValue))
	}

	// Validate the post-consensus message differently depending on a message type.
	validateMsg := func() error {
		decidedValue := &spectypes.ValidatorConsensusData{}
		if err := decidedValue.Decode(decidedValueBytes); err != nil {
			return errors.Wrap(err, "failed to parse decided value to ValidatorConsensusData")
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
				return errors.Wrap(err, "failed to parse decided value to BeaconVote")
			}

			// Use b.State.CurrentDuty.DutySlot() since CurrentDuty never changes for CommitteeRunner
			// by design, hence there is no need to store slot number on decidedValue for CommitteeRunner.
			expectedSlot := b.State.CurrentDuty.DutySlot()
			return b.validatePartialSigMsg(psigMsgs, expectedSlot)
		}
	}

	return validateMsg()
}

func (b *BaseRunner) validateDecidedConsensusData(valueCheckFn specqbft.ProposedValueCheckF, val spectypes.Encoder) error {
	byts, err := val.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode decided value")
	}
	if err := valueCheckFn(byts); err != nil {
		return errors.Wrap(err, "decided value is invalid")
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
		epoch := b.NetworkConfig.EstimatedEpochAtSlot(b.State.CurrentDuty.DutySlot())
		d, err := runner.GetBeaconNode().DomainData(ctx, epoch, domain)
		if err != nil {
			return nil, errors.Wrap(err, "could not get pre consensus root domain")
		}

		ret := make([][32]byte, 0)
		for _, rootI := range expectedRootObjs {
			r, err := spectypes.ComputeETHSigningRoot(rootI, d)
			if err != nil {
				return nil, errors.Wrap(err, "could not compute ETH signing root")
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
		ret := make([][32]byte, 0)
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
