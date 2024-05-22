package instance

import (
	"bytes"

	"github.com/pkg/errors"
	"github.com/ssvlabs/ssv/logging/fields"
	qbft "github.com/ssvlabs/ssv/protocol/v2/genesisqbft"
	types "github.com/ssvlabs/ssv/protocol/v2/genesistypes"
	"go.uber.org/zap"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
)

// uponRoundChange process round change messages.
// Assumes round change message is valid!
func (i *Instance) uponRoundChange(
	logger *zap.Logger,
	instanceStartValue []byte,
	signedRoundChange *genesisspecqbft.SignedMessage,
	roundChangeMsgContainer *genesisspecqbft.MsgContainer,
	valCheck genesisspecqbft.ProposedValueCheckF,
) error {
	hasQuorumBefore := genesisspecqbft.HasQuorum(i.State.Share, roundChangeMsgContainer.MessagesForRound(signedRoundChange.Message.
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
		fields.Round(specqbft.Round(i.State.Round)),
		fields.Height(specqbft.Height(i.State.Height)),
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
			fields.Round(specqbft.Round(i.State.Round)),
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

func (i *Instance) uponChangeRoundPartialQuorum(logger *zap.Logger, newRound genesisspecqbft.Round, instanceStartValue []byte) error {
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil

	i.config.GetTimer().TimeoutForRound(i.State.Height, i.State.Round)

	roundChange, err := CreateRoundChange(i.State, i.config, newRound, instanceStartValue)
	if err != nil {
		return errors.Wrap(err, "failed to create round change message")
	}

	logger.Debug("📢 got partial quorum, broadcasting round change message",
		fields.Round(specqbft.Round(i.State.Round)),
		fields.Root(roundChange.Message.Root),
		zap.Any("round-change-signers", roundChange.Signers),
		fields.Height(specqbft.Height(i.State.Height)),
		zap.String("reason", "partial-quorum"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}

func hasReceivedPartialQuorum(state *genesisspecqbft.State, roundChangeMsgContainer *genesisspecqbft.MsgContainer) (bool, []*genesisspecqbft.SignedMessage) {
	all := roundChangeMsgContainer.AllMessaged()

	rc := make([]*genesisspecqbft.SignedMessage, 0)
	for _, msg := range all {
		if msg.Message.Round > state.Round {
			rc = append(rc, msg)
		}
	}

	return genesisspecqbft.HasPartialQuorum(state.Share, rc), rc
}

// hasReceivedProposalJustificationForLeadingRound returns
// if first round or not received round change msgs with prepare justification - returns first rc msg in container and value to propose
// if received round change msgs with prepare justification - returns the highest prepare justification round change msg and value to propose
// (all the above considering the operator is a leader for the round
func hasReceivedProposalJustificationForLeadingRound(
	state *genesisspecqbft.State,
	config qbft.IConfig,
	instanceStartValue []byte,
	signedRoundChange *genesisspecqbft.SignedMessage,
	roundChangeMsgContainer *genesisspecqbft.MsgContainer,
	valCheck genesisspecqbft.ProposedValueCheckF,
) (*genesisspecqbft.SignedMessage, []byte, error) {
	roundChanges := roundChangeMsgContainer.MessagesForRound(signedRoundChange.Message.Round)
	// optimization, if no round change quorum can return false
	if !genesisspecqbft.HasQuorum(state.Share, roundChanges) {
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
	state *genesisspecqbft.State,
	config qbft.IConfig,
	roundChangeMsg *genesisspecqbft.SignedMessage,
	roundChanges []*genesisspecqbft.SignedMessage,
	roundChangeJustifications []*genesisspecqbft.SignedMessage,
	value []byte,
	valCheck genesisspecqbft.ProposedValueCheckF,
	newRound genesisspecqbft.Round,
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
	state *genesisspecqbft.State,
	config qbft.IConfig,
	roundChanges, prepares []*genesisspecqbft.SignedMessage,
	newRound genesisspecqbft.Round,
	value []byte,
	valCheck genesisspecqbft.ProposedValueCheckF,
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
	state *genesisspecqbft.State,
	config qbft.IConfig,
	signedMsg *genesisspecqbft.SignedMessage,
	height genesisspecqbft.Height,
	round genesisspecqbft.Round,
	fullData []byte,
) error {
	if signedMsg.Message.MsgType != genesisspecqbft.RoundChangeMsgType {
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

	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "roundChange invalid")
	}

	if !signedMsg.CheckSignersInCommittee(state.Share.Committee) {
		return errors.New("signer not in committee")
	}

	// Addition to formal spec
	// We add this extra tests on the msg itself to filter round change msgs with invalid justifications, before they are inserted into msg containers
	if signedMsg.Message.RoundChangePrepared() {
		r, err := genesisspecqbft.HashDataRoot(fullData)
		if err != nil {
			return errors.Wrap(err, "could not hash input data")
		}

		// validate prepare message justifications
		prepareMsgs, _ := signedMsg.Message.GetRoundChangeJustifications() // no need to check error, checked on signedMsg.Message.Validate()
		for _, pm := range prepareMsgs {
			if err := validSignedPrepareForHeightRoundAndRootVerifySignature(
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

		if !genesisspecqbft.HasQuorum(state.Share, prepareMsgs) {
			return errors.New("no justifications quorum")
		}

		if signedMsg.Message.DataRound > round {
			return errors.New("prepared round > round")
		}

		return nil
	}

	return nil
}

func validRoundChangeForDataVerifySignature(
	state *genesisspecqbft.State,
	config qbft.IConfig,
	signedMsg *genesisspecqbft.SignedMessage,
	height genesisspecqbft.Height,
	round genesisspecqbft.Round,
	fullData []byte,
) error {

	if err := validRoundChangeForDataIgnoreSignature(state, config, signedMsg, height, round, fullData); err != nil {
		return err
	}

	if config.VerifySignatures() {
		if err := types.VerifyByOperators(signedMsg.Signature, signedMsg, config.GetSignatureDomainType(), genesisspectypes.QBFTSignatureType, state.Share.Committee); err != nil {
			return errors.Wrap(err, "msg signature invalid")
		}
	}

	return nil
}

// highestPrepared returns a round change message with the highest prepared round, returns nil if none found
func highestPrepared(roundChanges []*genesisspecqbft.SignedMessage) (*genesisspecqbft.SignedMessage, error) {
	var ret *genesisspecqbft.SignedMessage
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
func minRound(roundChangeMsgs []*genesisspecqbft.SignedMessage) genesisspecqbft.Round {
	ret := genesisspecqbft.NoRound
	for _, msg := range roundChangeMsgs {
		if ret == genesisspecqbft.NoRound || ret > msg.Message.Round {
			ret = msg.Message.Round
		}
	}
	return ret
}

func getRoundChangeData(state *genesisspecqbft.State, config qbft.IConfig, instanceStartValue []byte) (genesisspecqbft.Round, [32]byte, []byte, []*genesisspecqbft.SignedMessage, error) {
	if state.LastPreparedRound != genesisspecqbft.NoRound && state.LastPreparedValue != nil {
		justifications, err := getRoundChangeJustification(state, config, state.PrepareContainer)
		if err != nil {
			return genesisspecqbft.NoRound, [32]byte{}, nil, nil, errors.Wrap(err, "could not get round change justification")
		}

		r, err := genesisspecqbft.HashDataRoot(state.LastPreparedValue)
		if err != nil {
			return genesisspecqbft.NoRound, [32]byte{}, nil, nil, errors.Wrap(err, "could not hash input data")
		}

		return state.LastPreparedRound, r, state.LastPreparedValue, justifications, nil
	}
	return genesisspecqbft.NoRound, [32]byte{}, nil, nil, nil
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
func CreateRoundChange(state *genesisspecqbft.State, config qbft.IConfig, newRound genesisspecqbft.Round, instanceStartValue []byte) (*genesisspecqbft.SignedMessage, error) {
	round, root, fullData, justifications, err := getRoundChangeData(state, config, instanceStartValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not generate round change data")
	}

	justificationsData, err := genesisspecqbft.MarshalJustifications(justifications)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}
	msg := &genesisspecqbft.Message{
		MsgType:    genesisspecqbft.RoundChangeMsgType,
		Height:     state.Height,
		Round:      newRound,
		Identifier: state.ID,

		Root:                     root,
		DataRound:                round,
		RoundChangeJustification: justificationsData,
	}
	sig, err := config.GetShareSigner().SignRoot(msg, genesisspectypes.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing round change msg")
	}

	signedMsg := &genesisspecqbft.SignedMessage{
		Signature: sig,
		Signers:   []genesisspectypes.OperatorID{state.Share.OperatorID},
		Message:   *msg,

		FullData: fullData,
	}
	return signedMsg, nil
}
