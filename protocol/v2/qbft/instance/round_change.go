package instance

import (
	"bytes"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
)

// uponRoundChange process round change messages.
// Assumes round change message is valid!
func (i *Instance) uponRoundChange(
	logger *zap.Logger,
	instanceStartValue []byte,
	signedRoundChange *spectypes.SignedSSVMessage,
	roundChangeMsgContainer *specqbft.MsgContainer,
	valCheck specqbft.ProposedValueCheckF,
) error {

	roundChangeMessage, err := specqbft.DecodeMessage(signedRoundChange.SSVMessage.Data)
	if err != nil {
		return err
	}

	hasQuorumBefore := specqbft.HasQuorum(i.State.CommitteeMember, roundChangeMsgContainer.MessagesForRound(roundChangeMessage.
		Round))
	// Currently, even if we have a quorum of round change messages, we update the container
	addedMsg, err := roundChangeMsgContainer.AddFirstMsgForSignerAndRound(signedRoundChange)
	if err != nil {
		return errors.Wrap(err, "could not add round change msg to container")
	}
	if !addedMsg {
		return nil // message was already added from signer
	}

	if hasQuorumBefore {
		return nil // already changed round
	}

	logger = logger.With(
		fields.Round(i.State.Round),
		fields.Height(i.State.Height),
		zap.Uint64("msg_round", uint64(roundChangeMessage.Round)),
	)

	logger.Debug("🔄 got round change",
		fields.Root(roundChangeMessage.Root),
		zap.Any("round-change-signers", signedRoundChange.OperatorIDs))

	signedJustifiedRoundChangeMsg, valueToPropose, err := hasReceivedProposalJustificationForLeadingRound(
		i.State,
		i.config,
		instanceStartValue,
		signedRoundChange,
		roundChangeMsgContainer,
		valCheck)
	if err != nil {
		return errors.Wrap(err, "could not get proposal justification for leading round")
	}

	if signedJustifiedRoundChangeMsg != nil {

		justifiedRoundChangeMsg, err := specqbft.DecodeMessage(signedJustifiedRoundChangeMsg.SSVMessage.Data)
		if err != nil {
			return err
		}

		roundChangeJustification, _ := justifiedRoundChangeMsg.GetRoundChangeJustifications() // no need to check error, check on isValidRoundChange

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

		r, _ := specqbft.HashDataRoot(valueToPropose) // TODO: err check although already happenes in createproposal

		logger.Debug("🔄 got justified round change, broadcasting proposal message",
			fields.Round(i.State.Round),
			zap.Any("round-change-signers", allSigners(roundChangeMsgContainer.MessagesForRound(i.State.Round))),
			fields.Root(r))

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

	root, err := specqbft.HashDataRoot(instanceStartValue)

	logger.Debug("📢 got partial quorum, broadcasting round change message",
		fields.Round(i.State.Round),
		fields.Root(root),
		zap.Any("round-change-signers", roundChange.OperatorIDs),
		fields.Height(i.State.Height),
		zap.String("reason", "partial-quorum"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}

func hasReceivedPartialQuorum(state *specqbft.State, roundChangeMsgContainer *specqbft.MsgContainer) (bool, []*spectypes.SignedSSVMessage) {
	all := roundChangeMsgContainer.AllMessages()

	rc := make([]*spectypes.SignedSSVMessage, 0)
	for _, signedMsg := range all {

		msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
		if err != nil {
			continue
		}

		if msg.Round > state.Round {
			rc = append(rc, signedMsg)
		}
	}

	return specqbft.HasPartialQuorum(state.CommitteeMember, rc), rc
}

// hasReceivedProposalJustificationForLeadingRound returns
// if first round or not received round change msgs with prepare justification - returns first rc msg in container and value to propose
// if received round change msgs with prepare justification - returns the highest prepare justification round change msg and value to propose
// (all the above considering the operator is a leader for the round
func hasReceivedProposalJustificationForLeadingRound(
	state *specqbft.State,
	config qbft.IConfig,
	instanceStartValue []byte,
	signedRoundChange *spectypes.SignedSSVMessage,
	roundChangeMsgContainer *specqbft.MsgContainer,
	valCheck specqbft.ProposedValueCheckF,
) (*spectypes.SignedSSVMessage, []byte, error) {

	roundChangeMsg, err := specqbft.DecodeMessage(signedRoundChange.SSVMessage.Data)
	if err != nil {
		return nil, nil, err
	}

	roundChanges := roundChangeMsgContainer.MessagesForRound(roundChangeMsg.Round)
	// optimization, if no round change quorum can return false
	if !specqbft.HasQuorum(state.CommitteeMember, roundChanges) {
		return nil, nil, nil
	}

	// Important!
	// We iterate on all round chance msgs for liveliness in case the last round change msg is malicious.
	for _, msg := range roundChanges {

		containerRoundChangeMessage, err := specqbft.DecodeMessage(msg.SSVMessage.Data)
		if err != nil {
			return nil, nil, err
		}

		// Chose proposal value.
		// If justifiedRoundChangeMsg has no prepare justification chose state value
		// If justifiedRoundChangeMsg has prepare justification chose prepared value
		valueToPropose := instanceStartValue
		if containerRoundChangeMessage.RoundChangePrepared() {
			valueToPropose = msg.FullData
		}

		roundChangeJustification, _ := containerRoundChangeMessage.GetRoundChangeJustifications() // no need to check error, checked on isValidRoundChange
		if isProposalJustificationForLeadingRound(
			state,
			config,
			msg,
			roundChanges,
			roundChangeJustification,
			valueToPropose,
			valCheck,
			roundChangeMsg.Round,
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
	roundChangeSignedMsg *spectypes.SignedSSVMessage,
	roundChanges []*spectypes.SignedSSVMessage,
	roundChangeJustifications []*spectypes.SignedSSVMessage,
	value []byte,
	valCheck specqbft.ProposedValueCheckF,
	newRound specqbft.Round,
) error {

	roundChangeMsg, err := specqbft.DecodeMessage(roundChangeSignedMsg.SSVMessage.Data)
	if err != nil {
		return err
	}

	if err := isReceivedProposalJustification(
		state,
		config,
		roundChanges,
		roundChangeJustifications,
		roundChangeMsg.Round,
		value,
		valCheck); err != nil {
		return err
	}

	if proposer(state, config, roundChangeMsg.Round) != state.CommitteeMember.OperatorID {
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
	roundChanges, prepares []*spectypes.SignedSSVMessage,
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

func validRoundChangeForDataIgnoreSignature(
	state *specqbft.State,
	config qbft.IConfig,
	signedMsg *spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {

	msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
	if err != nil {
		return err
	}

	if msg.MsgType != specqbft.RoundChangeMsgType {
		return errors.New("round change msg type is wrong")
	}
	if msg.Height != height {
		return errors.New("wrong msg height")
	}
	if msg.Round != round {
		return errors.New("wrong msg round")
	}
	if len(signedMsg.OperatorIDs) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "roundChange invalid")
	}

	if !signedMsg.CheckSignersInCommittee(state.CommitteeMember.Committee) {
		return errors.New("signer not in committee")
	}

	// Addition to formal spec
	// We add this extra tests on the msg itself to filter round change msgs with invalid justifications, before they are inserted into msg containers
	if msg.RoundChangePrepared() {
		r, err := specqbft.HashDataRoot(fullData)
		if err != nil {
			return errors.Wrap(err, "could not hash input data")
		}

		// validate prepare message justifications
		prepareMsgs, _ := msg.GetRoundChangeJustifications() // no need to check error, checked on msg.Validate()
		for _, pm := range prepareMsgs {
			if err := validSignedPrepareForHeightRoundAndRootVerifySignature(
				config,
				pm,
				state.Height,
				msg.DataRound,
				msg.Root,
				state.CommitteeMember.Committee); err != nil {
				return errors.Wrap(err, "round change justification invalid")
			}
		}

		if !bytes.Equal(r[:], msg.Root[:]) {
			return errors.New("H(data) != root")
		}

		if !specqbft.HasQuorum(state.CommitteeMember, prepareMsgs) {
			return errors.New("no justifications quorum")
		}

		if msg.DataRound > round {
			return errors.New("prepared round > round")
		}

		return nil
	}

	return nil
}

func validRoundChangeForDataVerifySignature(
	state *specqbft.State,
	config qbft.IConfig,
	signedMsg *spectypes.SignedSSVMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {

	if err := validRoundChangeForDataIgnoreSignature(state, config, signedMsg, height, round, fullData); err != nil {
		return err
	}

	if config.VerifySignatures() {
		// Verify signature
		if err := config.GetSignatureVerifier().Verify(signedMsg, state.CommitteeMember.Committee); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
	}

	return nil
}

// highestPrepared returns a round change message with the highest prepared round, returns nil if none found
func highestPrepared(roundChanges []*spectypes.SignedSSVMessage) (*spectypes.SignedSSVMessage, error) {
	var ret *spectypes.SignedSSVMessage
	var highestPreparedRound specqbft.Round
	for _, rc := range roundChanges {

		msg, err := specqbft.DecodeMessage(rc.SSVMessage.Data)
		if err != nil {
			continue
		}

		if !msg.RoundChangePrepared() {
			continue
		}

		if ret == nil {
			ret = rc
			highestPreparedRound = msg.DataRound
		} else {
			if highestPreparedRound < msg.DataRound {
				ret = rc
				highestPreparedRound = msg.DataRound
			}
		}
	}
	return ret, nil
}

// returns the min round number out of the signed round change messages and the current round
func minRound(roundChangeMsgs []*spectypes.SignedSSVMessage) specqbft.Round {
	ret := specqbft.NoRound
	for _, signedMsg := range roundChangeMsgs {
		msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
		if err != nil {
			continue
		}
		if ret == specqbft.NoRound || ret > msg.Round {
			ret = msg.Round
		}
	}
	return ret
}

func getRoundChangeData(state *specqbft.State, config qbft.IConfig, instanceStartValue []byte) (specqbft.Round, [32]byte, []byte, []*spectypes.SignedSSVMessage, error) {
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
func CreateRoundChange(state *specqbft.State, config qbft.IConfig, newRound specqbft.Round, instanceStartValue []byte) (*spectypes.SignedSSVMessage, error) {
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

	signedMsg, err := specqbft.Sign(msg, state.CommitteeMember.OperatorID, config.GetOperatorSigner())
	if err != nil {
		return nil, errors.Wrap(err, "could not sign round change message")
	}
	signedMsg.FullData = fullData
	return signedMsg, nil
}
