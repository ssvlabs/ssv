package ssv

import (
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

// ProcessMessage processes network Messages of all types
func (v *Validator) ProcessMessage(msg *types.SSVMessage) error {
	dutyRunner := v.DutyRunners.DutyRunnerForMsgID(msg.GetID())
	if dutyRunner == nil {
		return errors.Errorf("could not get duty runner for msg ID")
	}

	if err := v.validateMessage(dutyRunner, msg); err != nil {
		return errors.Wrap(err, "Messages invalid")
	}

	switch msg.GetType() {
	case types.SSVConsensusMsgType:
		signedMsg := &qbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get consensus Messages from network Messages")
		}
		return v.processConsensusMsg(dutyRunner, signedMsg)
	case types.SSVDecidedMsgType:
		decidedMsg := &qbft.DecidedMessage{}
		if err := decidedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get decided Messages from network Messages")
		}
		return v.processConsensusMsg(dutyRunner, decidedMsg.SignedMessage)
	case types.SSVPartialSignatureMsgType:
		signedMsg := &SignedPartialSignatureMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return errors.Wrap(err, "could not get post consensus Messages from network Messages")
		}

		if signedMsg.Type == RandaoPartialSig {
			return v.processRandaoPartialSig(dutyRunner, signedMsg)
		}
		if signedMsg.Type == SelectionProofPartialSig {
			return v.processSelectionProofPartialSig(dutyRunner, signedMsg)
		}
		if signedMsg.Type == ContributionProofs {
			return v.processContributionProofPartialSig(dutyRunner, signedMsg)
		}

		return v.processPostConsensusSig(dutyRunner, signedMsg)
	default:
		return errors.New("unknown msg")
	}
}

func (v *Validator) validateMessage(runner *Runner, msg *types.SSVMessage) error {
	if runner.CurrentDuty == nil {
		return errors.New("no running duty")
	}

	if !v.share.ValidatorPubKey.MessageIDBelongs(msg.GetID()) {
		return errors.New("msg ID doesn't match validator ID")
	}

	if len(msg.GetData()) == 0 {
		return errors.New("msg data is invalid")
	}

	return nil
}

func (v *Validator) processConsensusMsg(dutyRunner *Runner, msg *qbft.SignedMessage) error {
	decided, decidedValue, err := dutyRunner.ProcessConsensusMessage(msg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	postConsensusMsg, err := dutyRunner.SignDutyPostConsensus(decidedValue, v.signer)
	if err != nil {
		return errors.Wrap(err, "failed to decide duty at runner")
	}

	signedMsg, err := v.signPostConsensusMsg(postConsensusMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post consensus msg")
	}

	data, err := signedMsg.Encode()
	if err != nil {
		return errors.Wrap(err, "failed to encode post consensus signature msg")
	}

	msgToBroadcast := &types.SSVMessage{
		MsgType: types.SSVPartialSignatureMsgType,
		MsgID:   types.NewMsgID(v.share.ValidatorPubKey, dutyRunner.BeaconRoleType),
		Data:    data,
	}

	if err := v.network.Broadcast(msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}
	return nil
}

func (v *Validator) processPostConsensusSig(dutyRunner *Runner, signedMsg *SignedPartialSignatureMessage) error {
	quorum, roots, err := dutyRunner.ProcessPostConsensusMessage(signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	for _, r := range roots {
		switch dutyRunner.BeaconRoleType {
		case types.BNRoleAttester:
			att, err := dutyRunner.State.ReconstructAttestationSig(r, v.share.ValidatorPubKey)
			if err != nil {
				return errors.Wrap(err, "could not reconstruct post consensus sig")
			}
			if err := v.beacon.SubmitAttestation(att); err != nil {
				return errors.Wrap(err, "could not submit to beacon chain reconstructed attestation")
			}
			dutyRunner.State.Finished = true
		case types.BNRoleProposer:
			blk, err := dutyRunner.State.ReconstructBeaconBlockSig(r, v.share.ValidatorPubKey)
			if err != nil {
				return errors.Wrap(err, "could not reconstruct post consensus sig")
			}
			if err := v.beacon.SubmitBeaconBlock(blk); err != nil {
				return errors.Wrap(err, "could not submit to beacon chain reconstructed signed beacon block")
			}
			dutyRunner.State.Finished = true
		case types.BNRoleAggregator:
			msg, err := dutyRunner.State.ReconstructSignedAggregateSelectionProofSig(r, v.share.ValidatorPubKey)
			if err != nil {
				return errors.Wrap(err, "could not reconstruct post consensus sig")
			}
			if err := v.beacon.SubmitSignedAggregateSelectionProof(msg); err != nil {
				return errors.Wrap(err, "could not submit to beacon chain reconstructed signed aggregate")
			}
			dutyRunner.State.Finished = true
		case types.BNRoleSyncCommittee:
			msg, err := dutyRunner.State.ReconstructSyncCommitteeSig(r, v.share.ValidatorPubKey)
			if err != nil {
				return errors.Wrap(err, "could not reconstruct post consensus sig")
			}
			if err := v.beacon.SubmitSyncMessage(msg); err != nil {
				return errors.Wrap(err, "could not submit to beacon chain reconstructed signed sync committee")
			}
			dutyRunner.State.Finished = true
		case types.BNRoleSyncCommitteeContribution:
			signedContrib, err := dutyRunner.State.ReconstructContributionSig(r, v.share.ValidatorPubKey)
			if err != nil {
				return errors.Wrap(err, "could not reconstruct contribution and proof sig")
			}
			if err := v.beacon.SubmitSignedContributionAndProof(signedContrib); err != nil {
				return errors.Wrap(err, "could not submit to beacon chain reconstructed contribution and proof")
			}
			dutyRunner.State.Finished = true
		default:
			return errors.Errorf("unknown duty post consensus sig %s", dutyRunner.BeaconRoleType.String())
		}
	}
	return nil
}

func (v *Validator) processRandaoPartialSig(dutyRunner *Runner, signedMsg *SignedPartialSignatureMessage) error {
	quorum, roots, err := dutyRunner.ProcessRandaoMessage(signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing randao message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	for _, r := range roots {
		// randao is relevant only for block proposals, no need to check type
		fullSig, err := dutyRunner.State.ReconstructRandaoSig(r, v.share.ValidatorPubKey)
		if err != nil {
			return errors.Wrap(err, "could not reconstruct randao sig")
		}

		duty := dutyRunner.CurrentDuty

		// get block data
		blk, err := v.beacon.GetBeaconBlock(duty.Slot, duty.CommitteeIndex, v.share.Graffiti, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to get beacon block")
		}

		input := &types.ConsensusData{
			Duty:      duty,
			BlockData: blk,
		}

		if err := dutyRunner.Decide(input); err != nil {
			return errors.Wrap(err, "can't start new duty runner instance for duty")
		}
	}

	return nil
}

func (v *Validator) processSelectionProofPartialSig(dutyRunner *Runner, signedMsg *SignedPartialSignatureMessage) error {
	quorum, roots, err := dutyRunner.ProcessSelectionProofMessage(signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	for _, r := range roots {
		// reconstruct selection proof sig
		fullSig, err := dutyRunner.State.ReconstructSelectionProofSig(r, v.share.ValidatorPubKey)
		if err != nil {
			return errors.Wrap(err, "could not reconstruct selection proof sig")
		}

		duty := dutyRunner.CurrentDuty

		// TODO waitToSlotTwoThirds

		// get block data
		res, err := v.beacon.SubmitAggregateSelectionProof(duty.Slot, duty.CommitteeIndex, fullSig)
		if err != nil {
			return errors.Wrap(err, "failed to submit aggregate and proof")
		}

		input := &types.ConsensusData{
			Duty:              duty,
			AggregateAndProof: res,
		}

		if err := dutyRunner.Decide(input); err != nil {
			return errors.Wrap(err, "can't start new duty runner instance for duty")
		}
	}

	return nil
}

func (v *Validator) processContributionProofPartialSig(dutyRunner *Runner, signedMsg *SignedPartialSignatureMessage) error {
	quorum, roots, err := dutyRunner.ProcessContributionProofsMessage(signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing contribution proof message")
	}

	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// TODO - what happens if we get quorum multiple times?

	duty := dutyRunner.CurrentDuty
	input := &types.ConsensusData{
		Duty:                      duty,
		SyncCommitteeContribution: make(map[phase0.BLSSignature]*altair.SyncCommitteeContribution),
	}
	for _, r := range roots {
		// reconstruct selection proof sig
		sig, index, err := dutyRunner.State.ReconstructContributionProofSig(r, v.share.ValidatorPubKey)
		if err != nil {
			continue
		}

		aggregator, err := v.beacon.IsSyncCommitteeAggregator(sig)
		if err != nil {
			// can still continue, no need to fail
			continue
		}
		if !aggregator {
			continue
		}

		// fetch sync committee contribution
		subnet, err := v.beacon.SyncCommitteeSubnetID(index)
		contribution, err := v.beacon.GetSyncCommitteeContribution(duty.Slot, subnet, dutyRunner.CurrentDuty.PubKey)
		if err != nil {
			// can still continue, no need to fail
			continue
		}

		blsSig := phase0.BLSSignature{}
		copy(blsSig[:], sig)
		input.SyncCommitteeContribution[blsSig] = contribution
	}

	if err := dutyRunner.Decide(input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (v *Validator) signPostConsensusMsg(msg PartialSignatureMessages) (*SignedPartialSignatureMessage, error) {
	signature, err := v.signer.SignRoot(msg, types.PartialSignatureType, v.share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign PartialSignatureMessage for PostConsensusPartialSig")
	}

	return &SignedPartialSignatureMessage{
		Type:      PostConsensusPartialSig,
		Messages:  msg,
		Signature: signature,
		Signers:   []types.OperatorID{v.share.OperatorID},
	}, nil
}
