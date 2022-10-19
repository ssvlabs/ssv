package instance

import (
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

func (i *Instance) uponRoundChange(
	instanceStartValue []byte,
	signedRoundChange *qbftspec.SignedMessage,
	roundChangeMsgContainer *qbftspec.MsgContainer,
	valCheck qbftspec.ProposedValueCheckF,
) error {
	if err := validRoundChange(i.State, i.config, signedRoundChange, i.State.Height, signedRoundChange.Message.Round); err != nil {
		return errors.Wrap(err, "round change msg invalid")
	}

	addedMsg, err := roundChangeMsgContainer.AddFirstMsgForSignerAndRound(signedRoundChange)
	if err != nil {
		return errors.Wrap(err, "could not add round change msg to container")
	}
	if !addedMsg {
		return nil // UponCommit was already called
	}

	justifiedRoundChangeMsg, err := hasReceivedProposalJustificationForLeadingRound(
		i.State,
		i.config,
		signedRoundChange,
		roundChangeMsgContainer,
		valCheck)
	if err != nil {
		return errors.Wrap(err, "could not get proposal justification for leading ronud")
	}
	if justifiedRoundChangeMsg != nil {
		highestRCData, err := justifiedRoundChangeMsg.Message.GetRoundChangeData()
		if err != nil {
			return errors.Wrap(err, "could not round change data from highestJustifiedRoundChangeMsg")
		}

		// Chose proposal value.
		// If justifiedRoundChangeMsg has no prepare justification chose state value
		// If justifiedRoundChangeMsg has prepare justification chose prepared value
		valueToPropose := instanceStartValue
		if highestRCData.Prepared() {
			valueToPropose = highestRCData.PreparedValue
		}

		proposal, err := CreateProposal(
			i.State,
			i.config,
			valueToPropose,
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

	return HasPartialQuorum(state.Share, rc), rc
}

// hasReceivedProposalJustificationForLeadingRound returns
// if first round or not received round change msgs with prepare justification - returns first rc msg in container
// if received round change msgs with prepare justification - returns the highest prepare justification round change msg
// (all the above considering the operator is a leader for the round
func hasReceivedProposalJustificationForLeadingRound(
	state *State,
	config IConfig,
	signedRoundChange *SignedMessage,
	roundChangeMsgContainer *MsgContainer,
	valCheck ProposedValueCheckF,
) (*SignedMessage, error) {
	roundChanges := roundChangeMsgContainer.MessagesForRound(state.Round)

	// optimization, if no round change quorum can return false
	if !HasQuorum(state.Share, roundChanges) {
		return nil, nil
	}

	// Important!
	// We iterate on all round chance msgs for liveliness in case the last round change msg is malicious.
	for _, msg := range roundChanges {
		if isReceivedProposalJustificationForLeadingRound(
			state,
			config,
			msg,
			roundChanges,
			valCheck,
			signedRoundChange.Message.Round,
		) == nil {
			// not returning error, no need to
			return msg, nil
		}
	}
	return nil, nil
}

// isReceivedProposalJustificationForLeadingRound - returns nil if we have a quorum of round change msgs and highest justified value for leading round
func isReceivedProposalJustificationForLeadingRound(
	state *State,
	config IConfig,
	roundChangeMsg *SignedMessage,
	roundChanges []*SignedMessage,
	valCheck ProposedValueCheckF,
	newRound Round,
) error {
	rcData, err := roundChangeMsg.Message.GetRoundChangeData()
	if err != nil {
		return errors.Wrap(err, "could not get round change data")
	}

	if err := isReceivedProposalJustification(
		state,
		config,
		roundChanges,
		rcData.RoundChangeJustification,
		roundChangeMsg.Message.Round,
		rcData.PreparedValue,
		valCheck); err != nil {
		return err
	}

	if proposer(state, config, roundChangeMsg.Message.Round) != state.Share.OperatorID {
		return errors.New("not proposer")
	}

	currentRoundProposal := state.ProposalAcceptedForCurrentRound == nil && state.Round == newRound
	futureRoundProposal := newRound > state.Round

	if !currentRoundProposal && !futureRoundProposal {
		return errors.New("proposal round mismatch")
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
	valCheck ProposedValueCheckF,
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
		return errors.Wrap(err, "proposal not justified")
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
		return errors.New("msg round wrong")
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

	// Addition to formal spec
	// We add this extra tests on the msg itself to filter round change msgs with invalid justifications, before they are inserted into msg containers
	if rcData.Prepared() {
		// validate prepare message justifications
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

		if !HasQuorum(state.Share, prepareMsgs) {
			return errors.New("no justifications quorum")
		}

		if rcData.PreparedRound > round {
			return errors.New("prepared round > round")
		}

		return nil
	}
	return nil
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
			RoundChangeJustification: justifications,
		}, nil
	}
	return &RoundChangeData{
		PreparedRound: NoRound,
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
