package runner

import (
	"bytes"
	"context"
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
	signedMsg *spectypes.PartialSignatureMessages,
) error {
	if !b.hasRunningDuty() {
		return errors.New("no running duty")
	}

	if err := b.validatePartialSigMsgForSlot(signedMsg, b.State.StartingDuty.DutySlot()); err != nil {
		return err
	}

	roots, domain, err := runner.expectedPreConsensusRootsAndDomain()
	if err != nil {
		return err
	}

	return b.verifyExpectedRoot(ctx, runner, signedMsg, roots, domain)
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
		return errors.New("no running duty")
	}

	// TODO https://github.com/ssvlabs/ssv-spec/issues/142 need to fix with this issue solution instead.
	if len(b.State.DecidedValue) == 0 {
		return errors.New("no decided value")
	}

	if b.State.RunningInstance == nil {
		return errors.New("no running consensus instance")
	}
	decided, decidedValueBytes := b.State.RunningInstance.IsDecided()
	if !decided {
		return errors.New("consensus instance not decided")
	}

	// TODO: (Alan) maybe nicer to do this without switch
	switch runner.(type) {
	case *CommitteeRunner:
		decidedValue := &spectypes.BeaconVote{}
		if err := decidedValue.Decode(decidedValueBytes); err != nil {
			return errors.Wrap(err, "failed to parse decided value to BeaconData")
		}

		return b.validatePartialSigMsgForSlot(psigMsgs, b.State.StartingDuty.DutySlot())
	default:
		decidedValue := &spectypes.ValidatorConsensusData{}
		if err := decidedValue.Decode(decidedValueBytes); err != nil {
			return errors.Wrap(err, "failed to parse decided value to ValidatorConsensusData")
		}

		if err := b.validatePartialSigMsgForSlot(psigMsgs, decidedValue.Duty.Slot); err != nil {
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
		return errors.New("wrong expected roots count")
	}

	// convert expected roots to map and mark unique roots when verified
	sortedExpectedRoots, err := func(expectedRootObjs []ssz.HashRoot) ([][32]byte, error) {
		epoch := b.NetworkConfig.EstimatedEpochAtSlot(b.State.StartingDuty.DutySlot())
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
			return errors.New("wrong signing root")
		}
	}
	return nil
}
