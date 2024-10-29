package instance

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

// uponProposal process proposal message
// Assumes proposal message is valid!
func (i *Instance) uponProposal(logger *zap.Logger, msg *specqbft.ProcessingMessage, proposeMsgContainer *specqbft.MsgContainer) error {
	addedMsg, err := proposeMsgContainer.AddFirstMsgForSignerAndRound(msg)
	if err != nil {
		return errors.Wrap(err, "could not add proposal msg to container")
	}
	if !addedMsg {
		return nil // uponProposal was already called
	}

	logger = logger.With(
		fields.Height(i.State.Height),
		fields.Round(i.State.Round),
		zap.Uint64("msg_round", uint64(msg.QBFTMessage.Round)),
	)

	logger.Debug("📬 got proposal message",
		zap.Any("proposal_signers", msg.SignedMessage.OperatorIDs))

	i.State.ProposalAcceptedForCurrentRound = msg

	// A future justified proposal should bump us into future round and reset timer
	if msg.QBFTMessage.Round > i.State.Round {
		i.config.GetTimer().TimeoutForRound(msg.QBFTMessage.Height, msg.QBFTMessage.Round)
	}
	i.bumpToRound(msg.QBFTMessage.Round)

	i.metrics.EndStageProposal()

	// value root
	r, err := specqbft.HashDataRoot(msg.SignedMessage.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}

	prepare, err := i.CreatePrepare(i.signer, msg.QBFTMessage.Round, r)
	if err != nil {
		return errors.Wrap(err, "could not create prepare msg")
	}

	logger.Debug("📢 got proposal, broadcasting prepare message",
		zap.Any("proposal_signers", msg.SignedMessage.OperatorIDs),
		zap.Any("prepare_signers", prepare.OperatorIDs))

	if err := i.Broadcast(logger, prepare); err != nil {
		return errors.Wrap(err, "failed to broadcast prepare message")
	}

	return nil
}

func (i *Instance) validProposal(
	config qbft.IConfig,
	msg *specqbft.ProcessingMessage,
	valCheck specqbft.ProposedValueCheckF,
) error {
	if msg.QBFTMessage.MsgType != specqbft.ProposalMsgType {
		return errors.New("msg type is not proposal")
	}
	if msg.QBFTMessage.Height != i.State.Height {
		return errors.New("wrong msg height")
	}
	if len(msg.SignedMessage.OperatorIDs) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if !msg.SignedMessage.CheckSignersInCommittee(i.State.CommitteeMember.Committee) {
		return errors.New("signer not in committee")
	}

	if !msg.SignedMessage.MatchedSigners([]spectypes.OperatorID{i.proposer(config, msg.QBFTMessage.Round)}) {
		return errors.New("proposal leader invalid")
	}

	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "proposal invalid")
	}

	// verify full data integrity
	r, err := specqbft.HashDataRoot(msg.SignedMessage.FullData)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(msg.QBFTMessage.Root[:], r[:]) {
		return errors.New("H(data) != root")
	}

	// no need to check error, checked on msg.SignedMessage.Validate()
	roundChangeJustifications, _ := msg.QBFTMessage.GetRoundChangeJustifications()
	roundChangeJustificationsWrapped := make([]*specqbft.ProcessingMessage, 0, len(roundChangeJustifications))
	for _, rcSignedMessage := range roundChangeJustifications {
		rcWrapped, err := specqbft.NewProcessingMessage(rcSignedMessage)
		if err != nil {
			return fmt.Errorf("%w: %s", convertJustificationToProcessingMsgErr, err)
		}
		roundChangeJustificationsWrapped = append(roundChangeJustificationsWrapped, rcWrapped)
	}
	// no need to check error, checked on msg.SignedMessage.Validate()
	prepareJustificationSignedMessages, _ := msg.QBFTMessage.GetPrepareJustifications()
	prepareJustificationWrapped := make([]*specqbft.ProcessingMessage, 0, len(prepareJustificationSignedMessages))
	for _, prepareSignedMessage := range prepareJustificationSignedMessages {
		prepareWrapped, err := specqbft.NewProcessingMessage(prepareSignedMessage)
		if err != nil {
			return fmt.Errorf("%w: %s", convertJustificationToProcessingMsgErr, err)
		}
		prepareJustificationWrapped = append(prepareJustificationWrapped, prepareWrapped)
	}

	if err := i.isProposalJustification(
		roundChangeJustificationsWrapped,
		prepareJustificationWrapped,
		i.State.Height,
		msg.QBFTMessage.Round,
		msg.SignedMessage.FullData,
		valCheck,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}

	if (i.State.ProposalAcceptedForCurrentRound == nil && msg.QBFTMessage.Round == i.State.Round) ||
		msg.QBFTMessage.Round > i.State.Round {
		return nil
	}
	return errors.New("proposal is not valid with current state")
}

// isProposalJustification returns no error(s) if provided arguments can be combined
// to create a valid proposal, it verifies:
//   - whether proposedValue satisfies valCheck func (this func encapsulates external
//     constraints QBFT doesn't know about - such as deciding if duty data makes sense
//     or not)
//   - whether round change needs to happen (and if it is whether roundChangeMsgs allow
//     for round change to happen)
//   - whether proposedValue matches the highest(freshest) prepared value from previous
//     rounds (IF it exists) for operator cluster (in that case prepareMsgs must justify it)
func (i *Instance) isProposalJustification(
	roundChangeMsgs []*specqbft.ProcessingMessage,
	prepareMsgs []*specqbft.ProcessingMessage,
	height specqbft.Height,
	round specqbft.Round,
	proposedValue []byte,
	valCheck specqbft.ProposedValueCheckF,
) error {
	// first, check if proposed value even makes sense and error right away if not
	if err := valCheck(proposedValue); err != nil {
		return errors.Wrap(err, "proposal proposedValue invalid")
	}

	if round == specqbft.FirstRound {
		// when validating the value to propose, for the 1st round there can't be previously
		// prepared value (let alone justified prepared) to compare against, hence we are done
		return nil
	}

	// we are changing round then, check all round change messages we've got for this round

	for _, rc := range roundChangeMsgs {
		if err := i.validRoundChangeForDataVerifySignature(rc, height, round, proposedValue); err != nil {
			return errors.Wrap(err, "change round msg not valid")
		}
	}

	if !specqbft.HasQuorum(i.State.CommitteeMember, roundChangeMsgs) {
		return errors.New("change round has no quorum")
	}

	// previouslyPrepared returns true if any of the round change messages we've got claim
	// they (operator who sent message) have some prepared value (which isn't necessarily
	// for round-1, could be for round-2, ...)
	previouslyPrepared, err := func(rcMsgs []*specqbft.ProcessingMessage) (bool, error) {
		for _, rc := range rcMsgs {
			if rc.QBFTMessage.RoundChangePrepared() {
				return true, nil
			}
		}
		return false, nil
	}(roundChangeMsgs)
	if err != nil {
		return errors.Wrap(err, "could not check if previously prepared")
	}
	if !previouslyPrepared {
		return nil
	}

	// check prepare quorum
	if !specqbft.HasQuorum(i.State.CommitteeMember, prepareMsgs) {
		return errors.New("prepares has no quorum")
	}

	// get a round change data for which there is a justification for the highest previously prepared round
	rcMsg, err := highestPrepared(roundChangeMsgs)
	if err != nil {
		return errors.Wrap(err, "could not get highest prepared")
	}
	if rcMsg == nil {
		return errors.New("no highest prepared")
	}

	// proposed value must equal highest (freshest) prepared value for operator cluster
	r, err := specqbft.HashDataRoot(proposedValue)
	if err != nil {
		return errors.Wrap(err, "could not hash input data")
	}
	if !bytes.Equal(r[:], rcMsg.QBFTMessage.Root[:]) {
		return errors.New("proposed data doesn't match highest prepared")
	}

	// validate each prepare message against the highest previously prepared
	for _, pm := range prepareMsgs {
		if err := validSignedPrepareForHeightRoundAndRootVerifySignature(
			pm,
			height,
			rcMsg.QBFTMessage.DataRound,
			rcMsg.QBFTMessage.Root,
			i.State.CommitteeMember.Committee,
		); err != nil {
			return errors.New("signed prepare not valid")
		}
	}
	return nil
}

func (i *Instance) proposer(config qbft.IConfig, round specqbft.Round) spectypes.OperatorID {
	// TODO - https://github.com/ConsenSys/qbft-formal-spec-and-verification/blob/29ae5a44551466453a84d4d17b9e083ecf189d97/dafny/spec/L1/node_auxiliary_functions.dfy#L304-L323
	return config.GetProposerF()(i.State, round)
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
func (i *Instance) CreateProposal(signer ssvtypes.OperatorSigner, fullData []byte, roundChanges, prepares []*specqbft.ProcessingMessage) (*spectypes.SignedSSVMessage, error) {
	r, err := specqbft.HashDataRoot(fullData)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	roundChangeSignedMessages := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range roundChanges {
		roundChangeSignedMessages = append(roundChangeSignedMessages, msg.SignedMessage)
	}
	prepareSignedMessages := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range prepares {
		prepareSignedMessages = append(prepareSignedMessages, msg.SignedMessage)
	}

	roundChangesData, err := specqbft.MarshalJustifications(roundChangeSignedMessages)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}
	preparesData, err := specqbft.MarshalJustifications(prepareSignedMessages)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}

	msg := &specqbft.Message{
		MsgType:    specqbft.ProposalMsgType,
		Height:     i.State.Height,
		Round:      i.State.Round,
		Identifier: i.State.ID,

		Root:                     r,
		RoundChangeJustification: roundChangesData,
		PrepareJustification:     preparesData,
	}

	signedMsg, err := ssvtypes.Sign(msg, i.State.CommitteeMember.OperatorID, signer)
	if err != nil {
		return nil, errors.Wrap(err, "could not wrap proposal message")
	}
	signedMsg.FullData = fullData
	return signedMsg, nil
}
