package instance

import (
	"bytes"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

// uponRoundChange process round change messages.
// Assumes round change message is valid!
func (i *Instance) uponRoundChange(
	logger *zap.Logger,
	instanceStartValue []byte,
	signedRoundChange *specqbft.SignedMessage,
	roundChangeMsgContainer *specqbft.MsgContainer,
	valCheck specqbft.ProposedValueCheckF,
) error {
	addedMsg, err := roundChangeMsgContainer.AddFirstMsgForSignerAndRound(signedRoundChange)
	if err != nil {
		return errors.Wrap(err, "could not add round change msg to container")
	}
	if !addedMsg {
		return nil // UponCommit was already called
	}

	logger = logger.With(
		fields.Round(i.State.Round),
		fields.Height(i.State.Height),
		zap.Uint64("msg_round", uint64(signedRoundChange.Message.Round)),
	)

	logger.Debug("🔄 got round change",
		fields.Root(signedRoundChange.Message.Root),
		zap.Any("round-change-signers", signedRoundChange.Signers))

	justifiedRoundChangeMsg, valueToPropose, err := hasReceivedProposalJustificationForLeadingRound(
		i.State,
		i.config,
		instanceStartValue,
		signedRoundChange,
		roundChangeMsgContainer,
		valCheck)
	if err != nil {
		return errors.Wrap(err, "could not get proposal justification for leading round")
	}
	if justifiedRoundChangeMsg != nil {
		roundChangeJustification, _ := justifiedRoundChangeMsg.Message.GetRoundChangeJustifications() // no need to check error, check on isValidRoundChange

		proposal, err := CreateProposal(
			i.State,
			i.config,
			valueToPropose,
			roundChangeMsgContainer.MessagesForRound(i.State.Round), // TODO - might be optimized to include only necessary quorum
			roundChangeJustification,
		)
		if err != nil {
			return errors.Wrap(err, "failed to create proposal")
		}

		logger.Debug("🔄 got justified round change, broadcasting proposal message",
			fields.Round(i.State.Round),
			zap.Any("round-change-signers", allSigners(roundChangeMsgContainer.MessagesForRound(i.State.Round))),
			fields.Root(proposal.Message.Root))

		if err := i.Broadcast(logger, proposal); err != nil {
			return errors.Wrap(err, "failed to broadcast proposal message")
		}
	} else if partialQuorum, rcs := hasReceivedPartialQuorum(i.State, roundChangeMsgContainer); partialQuorum {
		newRound := minRound(rcs)
		if newRound <= i.State.Round {
			return nil // no need to advance round
		}
		err := i.uponChangeRoundPartialQuorum(logger, newRound, instanceStartValue)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) uponChangeRoundPartialQuorum(logger *zap.Logger, newRound specqbft.Round, instanceStartValue []byte) error {
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil

	i.config.GetTimer().TimeoutForRound(i.State.Height, i.State.Round)

	roundChange, err := CreateRoundChange(i.State, i.config, newRound, instanceStartValue)
	if err != nil {
		return errors.Wrap(err, "failed to create round change message")
	}

	logger.Debug("📢 got partial quorum, broadcasting round change message",
		fields.Round(i.State.Round),
		fields.Root(roundChange.Message.Root),
		zap.Any("round-change-signers", roundChange.Signers),
		fields.Height(i.State.Height),
		zap.String("reason", "partial-quorum"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}

func hasReceivedPartialQuorum(state *specqbft.State, roundChangeMsgContainer *specqbft.MsgContainer) (bool, []*specqbft.SignedMessage) {
	all := roundChangeMsgContainer.AllMessaged()

	rc := make([]*specqbft.SignedMessage, 0)
	for _, msg := range all {
		if msg.Message.Round > state.Round {
			rc = append(rc, msg)
		}
	}

	return specqbft.HasPartialQuorum(state.Share, rc), rc
}

// hasReceivedProposalJustificationForLeadingRound returns
// if first round or not received round change msgs with prepare justification - returns first rc msg in container and value to propose
// if received round change msgs with prepare justification - returns the highest prepare justification round change msg and value to propose
// (all the above considering the operator is a leader for the round
func hasReceivedProposalJustificationForLeadingRound(
	state *specqbft.State,
	config qbft.IConfig,
	instanceStartValue []byte,
	signedRoundChange *specqbft.SignedMessage,
	roundChangeMsgContainer *specqbft.MsgContainer,
	valCheck specqbft.ProposedValueCheckF,
) (*specqbft.SignedMessage, []byte, error) {
	roundChanges := roundChangeMsgContainer.MessagesForRound(signedRoundChange.Message.Round)
	// optimization, if no round change quorum can return false
	if !specqbft.HasQuorum(state.Share, roundChanges) {
		return nil, nil, nil
	}

	// Important!
	// We iterate on all round chance msgs for liveliness in case the last round change msg is malicious.
	for _, msg := range roundChanges {

		// Chose proposal value.
		// If justifiedRoundChangeMsg has no prepare justification chose state value
		// If justifiedRoundChangeMsg has prepare justification chose prepared value
		valueToPropose := instanceStartValue
		if msg.Message.RoundChangePrepared() {
			valueToPropose = signedRoundChange.FullData
		}

		roundChangeJustification, _ := msg.Message.GetRoundChangeJustifications() // no need to check error, checked on isValidRoundChange
		if isProposalJustificationForLeadingRound(
			state,
			config,
			msg,
			roundChanges,
			roundChangeJustification,
			valueToPropose,
			valCheck,
			signedRoundChange.Message.Round,
		) == nil {
			// not returning error, no need to
			return msg, valueToPropose, nil
		}
	}
	return nil, nil, nil
}

// isProposalJustificationForLeadingRound - returns nil if we have a quorum of round change msgs and highest justified value for leading round
func isProposalJustificationForLeadingRound(
	state *specqbft.State,
	config qbft.IConfig,
	roundChangeMsg *specqbft.SignedMessage,
	roundChanges []*specqbft.SignedMessage,
	roundChangeJustifications []*specqbft.SignedMessage,
	value []byte,
	valCheck specqbft.ProposedValueCheckF,
	newRound specqbft.Round,
) error {
	if err := isReceivedProposalJustification(
		state,
		config,
		roundChanges,
		roundChangeJustifications,
		roundChangeMsg.Message.Round,
		value,
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
	state *specqbft.State,
	config qbft.IConfig,
	roundChanges, prepares []*specqbft.SignedMessage,
	newRound specqbft.Round,
	value []byte,
	valCheck specqbft.ProposedValueCheckF,
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

func validRoundChangeForData(
	state *specqbft.State,
	config qbft.IConfig,
	signedMsg *specqbft.SignedMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {
	if signedMsg.Message.MsgType != specqbft.RoundChangeMsgType {
		return errors.New("round change msg type is wrong")
	}
	if signedMsg.Message.Height != height {
		return errors.New("wrong msg height")
	}
	if signedMsg.Message.Round != round {
		return errors.New("wrong msg round")
	}
	if len(signedMsg.GetSigners()) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if config.VerifySignatures() {
		if err := types.VerifyByOperators(signedMsg.Signature, signedMsg, config.GetSignatureDomainType(), spectypes.QBFTSignatureType, state.Share.Committee); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
	}

	if err := signedMsg.Message.Validate(); err != nil {
		return errors.Wrap(err, "roundChange invalid")
	}

	// Addition to formal spec
	// We add this extra tests on the msg itself to filter round change msgs with invalid justifications, before they are inserted into msg containers
	if signedMsg.Message.RoundChangePrepared() {
		r, err := specqbft.HashDataRoot(fullData)
		if err != nil {
			return errors.Wrap(err, "could not hash input data")
		}

		// validate prepare message justifications
		prepareMsgs, _ := signedMsg.Message.GetRoundChangeJustifications() // no need to check error, checked on signedMsg.Message.Validate()
		for _, pm := range prepareMsgs {
			if err := validSignedPrepareForHeightRoundAndRoot(
				config,
				pm,
				state.Height,
				signedMsg.Message.DataRound,
				signedMsg.Message.Root,
				state.Share.Committee); err != nil {
				return errors.Wrap(err, "round change justification invalid")
			}
		}

		if !bytes.Equal(r[:], signedMsg.Message.Root[:]) {
			return errors.New("H(data) != root")
		}

		if !specqbft.HasQuorum(state.Share, prepareMsgs) {
			return errors.New("no justifications quorum")
		}

		if signedMsg.Message.DataRound > round {
			return errors.New("prepared round > round")
		}

		return nil
	}
	return nil
}

// highestPrepared returns a round change message with the highest prepared round, returns nil if none found
func highestPrepared(roundChanges []*specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	var ret *specqbft.SignedMessage
	for _, rc := range roundChanges {
		if !rc.Message.RoundChangePrepared() {
			continue
		}

		if ret == nil {
			ret = rc
		} else {
			if ret.Message.DataRound < rc.Message.DataRound {
				ret = rc
			}
		}
	}
	return ret, nil
}

// returns the min round number out of the signed round change messages and the current round
func minRound(roundChangeMsgs []*specqbft.SignedMessage) specqbft.Round {
	ret := specqbft.NoRound
	for _, msg := range roundChangeMsgs {
		if ret == specqbft.NoRound || ret > msg.Message.Round {
			ret = msg.Message.Round
		}
	}
	return ret
}

func getRoundChangeData(state *specqbft.State, config qbft.IConfig, instanceStartValue []byte) (specqbft.Round, [32]byte, []byte, []*specqbft.SignedMessage, error) {
	if state.LastPreparedRound != specqbft.NoRound && state.LastPreparedValue != nil {
		justifications, err := getRoundChangeJustification(state, config, state.PrepareContainer)
		if err != nil {
			return specqbft.NoRound, [32]byte{}, nil, nil, errors.Wrap(err, "could not get round change justification")
		}

		r, err := specqbft.HashDataRoot(state.LastPreparedValue)
		if err != nil {
			return specqbft.NoRound, [32]byte{}, nil, nil, errors.Wrap(err, "could not hash input data")
		}

		return state.LastPreparedRound, r, state.LastPreparedValue, justifications, nil
	}
	return specqbft.NoRound, [32]byte{}, nil, nil, nil
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
func CreateRoundChange(state *specqbft.State, config qbft.IConfig, newRound specqbft.Round, instanceStartValue []byte) (*specqbft.SignedMessage, error) {
	round, root, fullData, justifications, err := getRoundChangeData(state, config, instanceStartValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not generate round change data")
	}

	justificationsData, err := specqbft.MarshalJustifications(justifications)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}
	msg := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     state.Height,
		Round:      newRound,
		Identifier: state.ID,

		Root:                     root,
		DataRound:                round,
		RoundChangeJustification: justificationsData,
	}
	sig, err := config.GetSigner().SignRoot(msg, spectypes.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing round change msg")
	}

	signedMsg := &specqbft.SignedMessage{
		Signature: sig,
		Signers:   []spectypes.OperatorID{state.Share.OperatorID},
		Message:   *msg,

		FullData: fullData,
	}
	return signedMsg, nil
}
