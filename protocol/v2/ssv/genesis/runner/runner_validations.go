package runner

import (
	"bytes"
	"sort"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	genesisspecssv "github.com/ssvlabs/ssv-spec-pre-cc/ssv"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

func (b *BaseRunner) ValidatePreConsensusMsg(runner Runner, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	if !b.hasRunningDuty() {
		return errors.New("no running duty")
	}

	if err := b.validatePartialSigMsgForSlot(signedMsg, b.State.StartingDuty.Slot); err != nil {
		return err
	}

	roots, domain, err := runner.expectedPreConsensusRootsAndDomain()
	if err != nil {
		return err
	}

	return b.verifyExpectedRoot(runner, signedMsg, roots, domain)
}

// Verify each signature in container removing the invalid ones
func (b *BaseRunner) FallBackAndVerifyEachSignature(container *genesisspecssv.PartialSigContainer, root [32]byte) {

	signatures := container.GetSignatures(root)

	for operatorID, signature := range signatures {
		if err := b.verifyBeaconPartialSignature(operatorID, signature, root); err != nil {
			container.Remove(operatorID, root)
		}
	}
}

func (b *BaseRunner) ValidatePostConsensusMsg(runner Runner, signedMsg *genesisspectypes.SignedPartialSignatureMessage) error {
	if !b.hasRunningDuty() {
		return errors.New("no running duty")
	}

	// TODO https://github.com/bloxapp/ssv-spec/issues/142 need to fix with this issue solution instead.
	if b.State.DecidedValue == nil {
		return errors.New("no decided value")
	}

	if b.State.RunningInstance == nil {
		return errors.New("no running consensus instance")
	}
	decided, decidedValueByts := b.State.RunningInstance.IsDecided()
	if !decided {
		return errors.New("consensus instance not decided")
	}

	decidedValue := &genesisspectypes.ConsensusData{}
	if err := decidedValue.Decode(decidedValueByts); err != nil {
		return errors.Wrap(err, "failed to parse decided value to ConsensusData")
	}

	if err := b.validatePartialSigMsgForSlot(signedMsg, decidedValue.Duty.Slot); err != nil {
		return err
	}

	roots, domain, err := runner.expectedPostConsensusRootsAndDomain()
	if err != nil {
		return err
	}

	return b.verifyExpectedRoot(runner, signedMsg, roots, domain)
}

func (b *BaseRunner) validateDecidedConsensusData(runner Runner, val *genesisspectypes.ConsensusData) error {
	byts, err := val.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode decided value")
	}
	if err := runner.GetValCheckF()(byts); err != nil {
		return errors.Wrap(err, "decided value is invalid")
	}

	return nil
}

func (b *BaseRunner) verifyExpectedRoot(runner Runner, signedMsg *genesisspectypes.SignedPartialSignatureMessage, expectedRootObjs []ssz.HashRoot, domain spec.DomainType) error {
	if len(expectedRootObjs) != len(signedMsg.Message.Messages) {
		return errors.New("wrong expected roots count")
	}

	// convert expected roots to map and mark unique roots when verified
	sortedExpectedRoots, err := func(expectedRootObjs []ssz.HashRoot) ([][32]byte, error) {
		epoch := b.BeaconNetwork.EstimatedEpochAtSlot(b.State.StartingDuty.Slot)
		d, err := runner.GetBeaconNode().DomainData(epoch, domain)
		if err != nil {
			return nil, errors.Wrap(err, "could not get pre consensus root domain")
		}

		ret := make([][32]byte, 0)
		for _, rootI := range expectedRootObjs {
			r, err := genesisspectypes.ComputeETHSigningRoot(rootI, d)
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

	sortedRoots := func(msgs genesisspectypes.PartialSignatureMessages) [][32]byte {
		ret := make([][32]byte, 0)
		for _, msg := range msgs.Messages {
			ret = append(ret, msg.SigningRoot)
		}

		sort.Slice(ret, func(i, j int) bool {
			return string(ret[i][:]) < string(ret[j][:])
		})
		return ret
	}(signedMsg.Message)

	// verify roots
	for i, r := range sortedRoots {
		if !bytes.Equal(sortedExpectedRoots[i][:], r[:]) {
			return errors.New("wrong signing root")
		}
	}
	return nil
}
