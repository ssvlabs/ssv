package instance

import (
	"bytes"
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	types2 "github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/pkg/errors"
)

func (i *Instance) uponProposal(signedProposal *qbftspec.SignedMessage, proposeMsgContainer *qbftspec.MsgContainer) error {
	valCheck := i.config.GetValueCheckF()
	if err := isValidProposal(i.State, i.config, signedProposal, valCheck, i.State.Share.Committee); err != nil {
		return errors.Wrap(err, "proposal invalid")
	}
	logex.GetLogger().Info("received valid proposal")
	addedMsg, err := proposeMsgContainer.AddFirstMsgForSignerAndRound(signedProposal)
	if err != nil {
		return errors.Wrap(err, "could not add proposal msg to container")
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	newRound := signedProposal.Message.Round
	i.State.ProposalAcceptedForCurrentRound = signedProposal

	// A future justified proposal should bump us into future round and reset timer
	if signedProposal.Message.Round > i.State.Round {
		i.config.GetTimer().TimeoutForRound(signedProposal.Message.Round)
	}
	i.State.Round = newRound

	proposalData, err := signedProposal.Message.GetProposalData()
	if err != nil {
		return errors.Wrap(err, "could not get proposal data")
	}

	prepare, err := CreatePrepare(i.State, i.config, newRound, proposalData.Data)
	if err != nil {
		return errors.Wrap(err, "could not create prepare msg")
	}

	if err := i.Broadcast(prepare); err != nil {
		return errors.Wrap(err, "failed to broadcast prepare message")
	}
	logex.GetLogger().Info("broadcast prepare")
	return nil
}

func isValidProposal(state *qbftspec.State, config types2.IConfig, signedProposal *qbftspec.SignedMessage, valCheck qbftspec.ProposedValueCheckF, operators []*types.Operator) error {
	if signedProposal.Message.MsgType != qbftspec.ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if signedProposal.Message.Height != state.Height {
		return errors.New("proposal Height is wrong")
	}
	if len(signedProposal.GetSigners()) != 1 {
		return errors.New("proposal msg allows 1 signer")
	}
	if err := signedProposal.Signature.VerifyByOperators(signedProposal, config.GetSignatureDomainType(), types.QBFTSignatureType, operators); err != nil {
		return errors.Wrap(err, "proposal msg signature invalid")
	}
	if !signedProposal.MatchedSigners([]types.OperatorID{proposer(state, config, signedProposal.Message.Round)}) {
		return errors.New("proposal leader invalid")
	}

	proposalData, err := signedProposal.Message.GetProposalData()
	if err != nil {
		return errors.Wrap(err, "could not get proposal data")
	}
	if err := proposalData.Validate(); err != nil {
		return errors.Wrap(err, "proposalData invalid")
	}

	if err := isProposalJustification(
		state,
		config,
		proposalData.RoundChangeJustification,
		proposalData.PrepareJustification,
		state.Height,
		signedProposal.Message.Round,
		proposalData.Data,
		valCheck,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (state.ProposalAcceptedForCurrentRound == nil && signedProposal.Message.Round == state.Round) ||
		signedProposal.Message.Round > state.Round {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

// isProposalJustification returns nil if the proposal and round change messages are valid and justify a proposal message for the provided round, value and leader
func isProposalJustification(state *qbftspec.State, config types2.IConfig, roundChangeMsgs []*qbftspec.SignedMessage, prepareMsgs []*qbftspec.SignedMessage, height qbftspec.Height, round qbftspec.Round, value []byte, valCheck qbftspec.ProposedValueCheckF) error {
	if err := valCheck(value); err != nil {
		return errors.Wrap(err, "proposal value invalid")
	}

	if round == qbftspec.FirstRound {
		return nil
	}
	// check all round changes are valid for height and round
	// no quorum, duplicate signers,  invalid still has quorum, invalid no quorum
	// prepared
	for _, rc := range roundChangeMsgs {
		if err := validRoundChange(state, config, rc, height, round); err != nil {
			return errors.Wrap(err, "change round msg not valid")
		}
	}

	// check there is a quorum
	if !qbftspec.HasQuorum(state.Share, roundChangeMsgs) {
		return errors.New("change round has no quorum")
	}

	// previouslyPreparedF returns true if any on the round change messages have a prepared round and value
	previouslyPrepared, err := func(rcMsgs []*qbftspec.SignedMessage) (bool, error) {
		for _, rc := range rcMsgs {
			rcData, err := rc.Message.GetRoundChangeData()
			if err != nil {
				return false, errors.Wrap(err, "could not get round change data")
			}
			if rcData.Prepared() {
				return true, nil
			}
		}
		return false, nil
	}(roundChangeMsgs)
	if err != nil {
		return errors.Wrap(err, "could not calculate if previously prepared")
	}

	if !previouslyPrepared {
		return nil
	}
	// check prepare quorum
	if !qbftspec.HasQuorum(state.Share, prepareMsgs) {
		return errors.New("prepares has no quorum")
	}

	// get a round change data for which there is a justification for the highest previously prepared round
	rcm, err := highestPrepared(roundChangeMsgs)
	if err != nil {
		return errors.Wrap(err, "could not get highest prepared")
	}
	if rcm == nil {
		return errors.New("no highest prepared")
	}
	rcmData, err := rcm.Message.GetRoundChangeData()
	if err != nil {
		return errors.Wrap(err, "could not get round change data")
	}

	// proposed value must equal highest prepared value
	if !bytes.Equal(value, rcmData.PreparedValue) {
		return errors.New("proposed data doesn't match highest prepared")
	}

	// validate each prepare message against the highest previously prepared value and round
	for _, pm := range prepareMsgs {
		if err := validSignedPrepareForHeightRoundAndValue(
			config,
			pm,
			height,
			rcmData.PreparedRound,
			rcmData.PreparedValue,
			state.Share.Committee,
		); err != nil {
			return errors.New("signed prepare not valid")
		}
	}
	return nil
}

func proposer(state *qbftspec.State, config types2.IConfig, round qbftspec.Round) types.OperatorID {
	// TODO - https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/29ae5a44551466453a84d4d17b9e083ecf189d97/dafny/spec/L1/node_auxiliary_functions.dfy#L304-L323
	return config.GetProposerF()(state, round)
}

// CreateProposal
/**
  	Proposal(
                        signProposal(
                            UnsignedProposal(
                                |current.blockchain|,
                                newRound,
                                digest(block)),
                            current.id),
                        block,
                        extractSignedRoundChanges(roundChanges),
                        extractSignedPrepares(prepares));
*/
func CreateProposal(state *qbftspec.State, config types2.IConfig, value []byte, roundChanges, prepares []*qbftspec.SignedMessage) (*qbftspec.SignedMessage, error) {
	proposalData := &qbftspec.ProposalData{
		Data:                     value,
		RoundChangeJustification: roundChanges,
		PrepareJustification:     prepares,
	}
	dataByts, err := proposalData.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode proposal data")
	}
	msg := &qbftspec.Message{
		MsgType:    qbftspec.ProposalMsgType,
		Height:     state.Height,
		Round:      state.Round,
		Identifier: state.ID,
		Data:       dataByts,
	}
	sig, err := config.GetSigner().SignRoot(msg, types.QBFTSignatureType, state.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed signing prepare msg")
	}

	signedMsg := &qbftspec.SignedMessage{
		Signature: sig,
		Signers:   []types.OperatorID{state.Share.OperatorID},
		Message:   msg,
	}
	return signedMsg, nil
}
