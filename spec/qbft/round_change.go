package qbft

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func uponRoundChange(
	state *State,
	config IConfig,
	signedRoundChange *SignedMessage,
	roundChangeMsgContainer *MsgContainer,
	valCheck ProposedValueCheck,
) error {
	// TODO - Roberto comment: could happen we received a round change before we switched the round and this msg will be rejected (lost)
	if err := validRoundChange(state, config, signedRoundChange, state.Height, state.Round); err != nil {
		return errors.Wrap(err, "round change msg invalid")
	}

	addedMsg, err := roundChangeMsgContainer.AddIfDoesntExist(signedRoundChange)
	if err != nil {
		return errors.Wrap(err, "could not add round change msg to container")
	}
	if !addedMsg {
		return nil // UponCommit was already called
	}

	if highestJustifiedRoundChangeMsg := hasReceivedProposalJustification(state, config, signedRoundChange, roundChangeMsgContainer, valCheck); highestJustifiedRoundChangeMsg != nil {
		// check if this node is the proposer
		if proposer(state, highestJustifiedRoundChangeMsg.Message.Round) != state.Share.OperatorID {
			return nil
		}

		proposal, err := createProposal(
			state,
			config,
			highestJustifiedRoundChangeMsg.Message.GetRoundChangeData().GetNextProposalData(),
			roundChangeMsgContainer.MessagesForRound(state.Round), // TODO - might be optimized to include only necessary quorum
			highestJustifiedRoundChangeMsg.Message.GetRoundChangeData().GetRoundChangeJustification(),
		)
		if err != nil {
			return errors.Wrap(err, "failed to create proposal")
		}

		if err := config.GetNetwork().Broadcast(proposal); err != nil {
			return errors.Wrap(err, "failed to broadcast proposal message")
		}
	} else if partialQuorum, rcs := hasReceivedPartialQuorum(state, roundChangeMsgContainer); partialQuorum {
		newRound := minRound(rcs)

		state.Round = newRound
		state.ProposalAcceptedForCurrentRound = nil

		roundChange := createRoundChange(state, newRound)
		if err := config.GetNetwork().Broadcast(roundChange); err != nil {
			return errors.Wrap(err, "failed to broadcast round change message")
		}
	}
	return nil
}

func hasReceivedPartialQuorum(state *State, roundChangeMsgContainer *MsgContainer) (bool, []*SignedMessage) {
	all := roundChangeMsgContainer.AllMessaged()

	rc := make([]*SignedMessage, 0)
	for _, msg := range all {
		if msg.Message.Round > state.Round {
			rc = append(rc, msg)
		}
	}

	return state.Share.HasPartialQuorum(len(rc)), rc
}

func hasReceivedProposalJustification(
	state *State,
	config IConfig,
	signedRoundChange *SignedMessage,
	roundChangeMsgContainer *MsgContainer,
	valCheck ProposedValueCheck,
) *SignedMessage {
	roundChanges := roundChangeMsgContainer.MessagesForRound(state.Round)

	// TODO - optimization, if no round change quorum can return false

	// Important!
	// We iterate on all round chance msgs for liveliness in case the last round change msg is malicious.
	for _, msg := range roundChanges {
		prepares := msg.Message.GetRoundChangeData().GetRoundChangeJustification()
		if isReceivedProposalJustification(
			state,
			config,
			roundChanges,
			prepares,
			signedRoundChange.Message.Round,
			msg.Message.GetRoundChangeData().GetNextProposalData(),
			valCheck,
		) != nil {
			return msg
		}
	}
	return nil
}

// isReceivedProposalJustification - returns nil if we have a quorum of round change msgs and highest justified value
func isReceivedProposalJustification(
	state *State,
	config IConfig,
	roundChanges, prepares []*SignedMessage,
	newRound Round,
	value []byte,
	valCheck ProposedValueCheck,
) error {
	if err := isProposalJustification(
		state,
		config,
		roundChanges,
		prepares,
		state.Height,
		newRound,
		value,
		valCheck,
		proposer(state, newRound),
	); err != nil {
		return errors.Wrap(err, "round change ")
	}

	noPrevProposal := state.ProposalAcceptedForCurrentRound == nil && state.Round == newRound
	prevProposal := state.ProposalAcceptedForCurrentRound != nil && newRound > state.Round

	if !noPrevProposal && !prevProposal {
		return errors.New("prev proposal and new round mismatch")
	}
	return nil
}

func validRoundChange(state *State, config IConfig, signedMsg *SignedMessage, height Height, round Round) error {
	if signedMsg.Message.MsgType != RoundChangeMsgType {
		return errors.New("round change msg type is wrong")
	}
	if signedMsg.Message.Height != height {
		return errors.New("round change Height is wrong")
	}
	if signedMsg.Message.Round != round {
		return errors.New("round change round is wrong")
	}

	if len(signedMsg.GetSigners()) != 1 {
		return errors.New("round change msg allows 1 signer")
	}

	if err := signedMsg.Signature.VerifyByOperators(signedMsg, config.GetSignatureDomainType(), types.QBFTSignatureType, state.Share.Committee); err != nil {
		return errors.Wrap(err, "round change msg signature invalid")
	}

	if err := signedMsg.Message.GetRoundChangeData().Validate(); err != nil {
		return errors.Wrap(err, "roundChangeData invalid")
	}
	if signedMsg.Message.GetRoundChangeData().GetPreparedRound() == NoRound &&
		signedMsg.Message.GetRoundChangeData().GetPreparedValue() == nil {
		return nil
	} else if signedMsg.Message.GetRoundChangeData().GetPreparedRound() != NoRound &&
		signedMsg.Message.GetRoundChangeData().GetPreparedValue() != nil {

		// TODO Roberto comment: we should add a validation for justification data (sigs and so on)

		if signedMsg.Message.GetRoundChangeData().GetPreparedRound() < round {
			return nil
		}
		return errors.New("prepared round >= round")
	}
	return errors.New("round change prepare round & value are wrong")
}

// highestPrepared returns a round change message with the highest prepared round, returns nil if none found
func highestPrepared(roundChanges []*SignedMessage) *SignedMessage {
	var ret *SignedMessage
	for _, rc := range roundChanges {
		if rc.Message.GetRoundChangeData().GetPreparedRound() == NoRound &&
			rc.Message.GetRoundChangeData().GetPreparedValue() == nil {
			continue
		}

		if ret == nil {
			ret = rc
		} else if ret.Message.GetRoundChangeData().GetPreparedRound() < rc.Message.GetRoundChangeData().GetPreparedRound() {
			ret = rc
		}
	}
	return ret
}

func minRound(roundChangeMsgs []*SignedMessage) Round {
	panic("implement")
}

func createRoundChange(state *State, newRound Round) *SignedMessage {
	/**
	RoundChange(
	           signRoundChange(
	               UnsignedRoundChange(
	                   |current.blockchain|,
	                   newRound,
	                   digestOptionalBlock(current.lastPreparedBlock),
	                   current.lastPreparedRound),
	           current.id),
	           current.lastPreparedBlock,
	           getRoundChangeJustification(current)
	       )
	*/
	panic("implement")
}
