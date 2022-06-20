package qbft

import (
	"github.com/bloxapp/ssv/spec/types"
	"github.com/pkg/errors"
)

func (i *Instance) uponRoundChange(
	instanceStartValue []byte,
	signedRoundChange *SignedMessage,
	roundChangeMsgContainer *MsgContainer,
	valCheck ProposedValueCheck,
) error {
	// TODO - Roberto comment: could happen we received a round change before we switched the round and this msg will be rejected (lost)
	if err := validRoundChange(i.State, i.config, signedRoundChange, i.State.Height, i.State.Round); err != nil {
		return errors.Wrap(err, "round change msg invalid")
	}

	addedMsg, err := roundChangeMsgContainer.AddIfDoesntExist(signedRoundChange)
	if err != nil {
		return errors.Wrap(err, "could not add round change msg to container")
	}
	if !addedMsg {
		return nil // UponCommit was already called
	}

	highestJustifiedRoundChangeMsg, err := hasReceivedProposalJustificationForLeadingRound(
		i.State,
		i.config,
		signedRoundChange,
		roundChangeMsgContainer,
		valCheck)
	if err != nil {
		return errors.Wrap(err, "could not get proposal justification for leading ronud")
	}
	if highestJustifiedRoundChangeMsg != nil {
		highestRCData, err := highestJustifiedRoundChangeMsg.Message.GetRoundChangeData()
		if err != nil {
			return errors.Wrap(err, "could not round change data from highestJustifiedRoundChangeMsg")
		}

		proposal, err := CreateProposal(
			i.State,
			i.config,
			highestRCData.NextProposalData,
			roundChangeMsgContainer.MessagesForRound(i.State.Round), // TODO - might be optimized to include only necessary quorum
			highestRCData.RoundChangeJustification,
		)
		if err != nil {
			return errors.Wrap(err, "failed to create proposal")
		}

		if err := i.Broadcast(proposal); err != nil {
			return errors.Wrap(err, "failed to broadcast proposal message")
		}
	} else if partialQuorum, rcs := hasReceivedPartialQuorum(i.State, roundChangeMsgContainer); partialQuorum {
		newRound := minRound(rcs)
		if newRound <= i.State.Round {
			return nil // no need to advance round
		}

		i.State.Round = newRound
		// TODO - should we reset timeout here for the new round?
		i.State.ProposalAcceptedForCurrentRound = nil

		roundChange, err := CreateRoundChange(i.State, i.config, newRound, instanceStartValue)
		if err != nil {
			return errors.Wrap(err, "failed to create round change message")
		}
		if err := i.Broadcast(roundChange); err != nil {
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

// hasReceivedProposalJustificationForLeadingRound returns the highest justified round change message (if this node is also a leader)
func hasReceivedProposalJustificationForLeadingRound(
	state *State,
	config IConfig,
	signedRoundChange *SignedMessage,
	roundChangeMsgContainer *MsgContainer,
	valCheck ProposedValueCheck,
) (*SignedMessage, error) {
	roundChanges := roundChangeMsgContainer.MessagesForRound(state.Round)
	// optimization, if no round change quorum can return false
	if !state.Share.HasQuorum(len(roundChanges)) {
		return nil, nil
	}

	// Important!
	// We iterate on all round chance msgs for liveliness in case the last round change msg is malicious.
	for _, msg := range roundChanges {
		rcData, err := msg.Message.GetRoundChangeData()
		if err != nil {
			return nil, errors.Wrap(err, "could not get round change data")
		}

		if isReceivedProposalJustification(
			state,
			config,
			roundChanges,
			rcData.RoundChangeJustification,
			signedRoundChange.Message.Round,
			rcData.NextProposalData,
			valCheck,
		) == nil &&
			proposer(state, msg.Message.Round) == state.Share.OperatorID {
			return msg, nil
		}
	}
	return nil, nil
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

	if len(signedMsg.GetSigners()) != 1 {
		return errors.New("round change msg allows 1 signer")
	}

	if err := signedMsg.Signature.VerifyByOperators(signedMsg, config.GetSignatureDomainType(), types.QBFTSignatureType, state.Share.Committee); err != nil {
		return errors.Wrap(err, "round change msg signature invalid")
	}

	rcData, err := signedMsg.Message.GetRoundChangeData()
	if err != nil {
		return errors.Wrap(err, "could not get roundChange data ")
	}
	if err := rcData.Validate(); err != nil {
		return errors.Wrap(err, "roundChangeData invalid")
	}

	if !rcData.Prepared() {
		return nil
	} else { // validate prepare message justifications
		prepareMsgs := rcData.RoundChangeJustification
		for _, pm := range prepareMsgs {
			if err := validSignedPrepareForHeightRoundAndValue(
				config,
				pm,
				state.Height,
				rcData.PreparedRound,
				rcData.PreparedValue,
				state.Share.Committee); err != nil {
				return errors.Wrap(err, "round change justification invalid")
			}
		}

		if rcData.PreparedRound <= round {
			return nil
		}
		return errors.New("prepared round > round")
	}
	return errors.New("round change prepare round & value are wrong")
}

// highestPrepared returns a round change message with the highest prepared round, returns nil if none found
func highestPrepared(roundChanges []*SignedMessage) (*SignedMessage, error) {
	var ret *SignedMessage
	for _, rc := range roundChanges {
		rcData, err := rc.Message.GetRoundChangeData()
		if err != nil {
			return nil, errors.Wrap(err, "could not get round change data")
		}

		if !rcData.Prepared() {
			continue
		}

		if ret == nil {
			ret = rc
		} else {
			retRCData, err := ret.Message.GetRoundChangeData()
			if err != nil {
				return nil, errors.Wrap(err, "could not get round change data")
			}
			if retRCData.PreparedRound < rcData.PreparedRound {
				ret = rc
			}
		}
	}
	return ret, nil
}

// returns the min round number out of the signed round change messages and the current round
func minRound(roundChangeMsgs []*SignedMessage) Round {
	ret := NoRound
	for _, msg := range roundChangeMsgs {
		if ret == NoRound || ret > msg.Message.Round {
			ret = msg.Message.Round
		}
	}
	return ret
}

func getRoundChangeData(state *State, config IConfig, instanceStartValue []byte) (*RoundChangeData, error) {
	if state.LastPreparedRound != NoRound && state.LastPreparedValue != nil {
		justifications := getRoundChangeJustification(state, config, state.PrepareContainer)
		return &RoundChangeData{
			PreparedRound:            state.LastPreparedRound,
			PreparedValue:            state.LastPreparedValue,
			NextProposalData:         state.LastPreparedValue,
			RoundChangeJustification: justifications,
		}, nil
	}
	return &RoundChangeData{
		PreparedRound:    NoRound,
		NextProposalData: instanceStartValue,
	}, nil
}

// CreateRoundChange
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
func CreateRoundChange(state *State, config IConfig, newRound Round, instanceStartValue []byte) (*SignedMessage, error) {
	rcData, err := getRoundChangeData(state, config, instanceStartValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not generate round change data")
	}
	dataByts, err := rcData.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode round change data")
	}

	msg := &Message{
		MsgType:    RoundChangeMsgType,
		Height:     state.Height,
		Round:      newRound,
		Identifier: state.ID,
		Data:       dataByts,
	}
	sig, err := config.GetSigner().SignRoot(msg, types.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing prepare msg")
	}

	signedMsg := &SignedMessage{
		Signature: sig,
		Signers:   []types.OperatorID{state.Share.OperatorID},
		Message:   msg,
	}
	return signedMsg, nil
}
