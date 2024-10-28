package instance

import (
	"bytes"
	errs "errors"
	"fmt"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

// uponRoundChange process round change messages.
// Assumes round change message is valid!
func (i *Instance) uponRoundChange(
	logger *zap.Logger,
	instanceStartValue []byte,
	msg *specqbft.ProcessingMessage,
	roundChangeMsgContainer *specqbft.MsgContainer,
	valCheck specqbft.ProposedValueCheckF,
) error {
	hasQuorumBefore := specqbft.HasQuorum(i.State.CommitteeMember, roundChangeMsgContainer.MessagesForRound(msg.QBFTMessage.Round))
	// Currently, even if we have a quorum of round change messages, we update the container
	addedMsg, err := roundChangeMsgContainer.AddFirstMsgForSignerAndRound(msg)
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
		zap.Uint64("msg_round", uint64(msg.QBFTMessage.Round)),
	)

	logger.Debug("🔄 got round change",
		fields.Root(msg.QBFTMessage.Root),
		zap.Any("round_change_signers", msg.SignedMessage.OperatorIDs))

	// 1) if our current round (i.State.Round) is stale we need to fix it

	partialQuorum, newRound := i.gotPartialQuorumAboveOwnRound(roundChangeMsgContainer)
	if partialQuorum && newRound > i.State.Round {
		// this code executes exactly once per round - when partial round quorum has
		// been achieved (because Instance events are processed sequentially)
		err = i.uponPartialQuorumAboveOwnRound(logger, newRound, instanceStartValue)
		if err != nil {
			return fmt.Errorf("could not process round change with partial quorum above own round: %w", err)
		}

		// we can stop here, there is no need to check if this message has achieved
		// round change quorum (for received round change message's round) because
		// this can't happen on the same message we achieved partial quorum above
		// our own round (or any prior messages for the same round, for that matter) -
		// this is because Instance processes every message received sequentially
		// (meaning no valid message is skipped or processed concurrently)
		return nil
	}

	// 2) if we have a quorum on the round from received message we should try to
	//    finish/end that round by proposing "appropriate value" if we (this operator)
	//    can lead such proposal

	roundChangesWrapped := roundChangeMsgContainer.MessagesForRound(msg.QBFTMessage.Round)
	if !specqbft.HasQuorum(i.State.CommitteeMember, roundChangesWrapped) {
		return nil
	}

	// code below executes exactly once per round when round quorum has been achieved
	// (because Instance events are processed sequentially)

	justifiedRoundChangeMsg, valueToPropose, err := i.buildJustifiedProposalToLeadRound(
		i.config,
		instanceStartValue,
		roundChangesWrapped,
		msg.QBFTMessage.Round,
		valCheck,
	)
	if errs.Is(err, noJustifiedProposalErr) {
		logger.Debug(
			"can't lead round change",
			fields.Root(msg.QBFTMessage.Root),
			zap.Any("round_change_signers", msg.SignedMessage.OperatorIDs),
			zap.Error(err),
		)
		return nil
	} else if err != nil {
		return fmt.Errorf("could not get proposal justification for leading round: %w", err)
	}

	// no need to check error, check on isValidRoundChange
	roundChangeJustifications, _ := justifiedRoundChangeMsg.QBFTMessage.GetRoundChangeJustifications()
	roundChangeJustificationWrapped := make([]*specqbft.ProcessingMessage, 0, len(roundChangeJustifications))
	for _, rcSignedMessage := range roundChangeJustifications {
		rcWrapped, err := specqbft.NewProcessingMessage(rcSignedMessage)
		if err != nil {
			return fmt.Errorf("%w: %s", convertJustificationToProcessingMsgErr, err)
		}
		roundChangeJustificationWrapped = append(roundChangeJustificationWrapped, rcWrapped)
	}

	proposal, err := i.CreateProposal(
		i.signer,
		valueToPropose,
		roundChangeMsgContainer.MessagesForRound(i.State.Round), // TODO - might be optimized to include only necessary quorum
		roundChangeJustificationWrapped,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create proposal")
	}

	r, err := specqbft.HashDataRoot(valueToPropose)
	if err != nil {
		return fmt.Errorf("couldn't calculate data root hash: %w", err)
	}

	logger.Debug("🔄 got justified round change, broadcasting proposal message",
		fields.Round(i.State.Round),
		zap.Any("round_change_signers", allSigners(roundChangeMsgContainer.MessagesForRound(i.State.Round))),
		fields.Root(r))

	if err := i.Broadcast(logger, proposal); err != nil {
		return errors.Wrap(err, "failed to broadcast proposal message")
	}

	return nil
}

// gotPartialQuorumAboveOwnRound returns whether there is a partial quorum of
// round change messages with rounds higher than current Instance round, also
// returns a new round this partial quorum supports (which is min round across
// this quorum).
func (i *Instance) gotPartialQuorumAboveOwnRound(roundChangeMsgContainer *specqbft.MsgContainer) (bool, specqbft.Round) {
	all := roundChangeMsgContainer.AllMessages()

	rcMsgsFiltered := make([]*specqbft.ProcessingMessage, 0)
	for _, msg := range all {
		if msg.QBFTMessage.Round > i.State.Round {
			rcMsgsFiltered = append(rcMsgsFiltered, msg)
		}
	}

	newRound := minRound(rcMsgsFiltered)

	return specqbft.HasPartialQuorum(i.State.CommitteeMember, rcMsgsFiltered), newRound
}

func (i *Instance) uponPartialQuorumAboveOwnRound(
	logger *zap.Logger,
	newRound specqbft.Round,
	instanceStartValue []byte,
) error {
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil

	i.config.GetTimer().TimeoutForRound(i.State.Height, i.State.Round)

	roundChange, err := i.CreateRoundChange(i.signer, newRound, instanceStartValue)
	if err != nil {
		return errors.Wrap(err, "failed to create round change message")
	}

	root, err := specqbft.HashDataRoot(instanceStartValue)
	if err != nil {
		return errors.Wrap(err, "failed to hash instance start value")
	}
	logger.Debug("📢 got partial quorum, broadcasting round change message",
		fields.Round(i.State.Round),
		fields.Root(root),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
		fields.Height(i.State.Height),
		zap.String("reason", "partial-quorum"))

	if err := i.Broadcast(logger, roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}

// buildJustifiedProposalToLeadRound returns
//   - (if first round or not received round change msgs with prepare justification) first
//     round change msg in roundChangesWrapped and value to propose
//   - (if received round change msgs with prepare justification) the highest prepared
//     justified round change msg and value to propose
//
// otherwise (unless there is an actual error that prevents this func from finishing)
// noJustifiedProposalErr error is returned to signal that justified proposal to finish/end
// specified round can't be built.
func (i *Instance) buildJustifiedProposalToLeadRound(
	config qbft.IConfig,
	instanceStartValue []byte,
	roundChangesWrapped []*specqbft.ProcessingMessage,
	round specqbft.Round,
	valCheck specqbft.ProposedValueCheckF,
) (*specqbft.ProcessingMessage, []byte, error) {
	var combinedErr error
	// Important! We iterate on all round change msgs for liveliness in case the last
	// round change msg is malicious.
	for _, roundChangeMsgWrapped := range roundChangesWrapped {
		// chose proposal value:
		// - if justifiedRoundChangeMsg has no prepare justification chose Instance start value
		// - if justifiedRoundChangeMsg has prepare justification chose prepared value
		valueToPropose := instanceStartValue
		if roundChangeMsgWrapped.QBFTMessage.RoundChangePrepared() {
			valueToPropose = roundChangeMsgWrapped.SignedMessage.FullData
		}

		// no need to check error, checked on isValidRoundChange
		roundChangeJustifications, _ := roundChangeMsgWrapped.QBFTMessage.GetRoundChangeJustifications()

		roundChangeJustificationsWrapped := make([]*specqbft.ProcessingMessage, 0, len(roundChangeJustifications))
		for _, signedMessage := range roundChangeJustifications {
			msgWrapped, err := specqbft.NewProcessingMessage(signedMessage)
			if err != nil {
				return nil, nil, fmt.Errorf("%w: %s", convertJustificationToProcessingMsgErr, err)
			}
			roundChangeJustificationsWrapped = append(roundChangeJustificationsWrapped, msgWrapped)
		}

		err := i.isProposalJustificationToLeadRound(
			config,
			roundChangeMsgWrapped,
			roundChangesWrapped,
			roundChangeJustificationsWrapped,
			valueToPropose,
			valCheck,
			round,
		)
		if err != nil {
			combinedErr = errs.Join(combinedErr, fmt.Errorf(
				"check proposal justificaiton for msg: %s, err: %w",
				roundChangeMsgWrapped.QBFTMessage.Identifier,
				err,
			))
			continue
		}
		return roundChangeMsgWrapped, valueToPropose, nil
	}
	return nil, nil, fmt.Errorf("%w: %s", noJustifiedProposalErr, combinedErr)
}

// isProposalJustificationToLeadRound - same as isProposalJustification but additionally
// checks that our operator can lead newRound.
func (i *Instance) isProposalJustificationToLeadRound(
	config qbft.IConfig,
	roundChangeMsg *specqbft.ProcessingMessage,
	roundChanges []*specqbft.ProcessingMessage,
	roundChangeJustifications []*specqbft.ProcessingMessage,
	value []byte,
	valCheck specqbft.ProposedValueCheckF,
	newRound specqbft.Round,
) error {
	if err := i.isProposalJustification(
		roundChanges,
		roundChangeJustifications,
		roundChangeMsg.QBFTMessage.Height,
		roundChangeMsg.QBFTMessage.Round,
		value,
		valCheck,
	); err != nil {
		return err
	}

	if i.proposer(config, roundChangeMsg.QBFTMessage.Round) != i.State.CommitteeMember.OperatorID {
		return errors.New("not proposer")
	}

	if i.State.Round != newRound {
		return fmt.Errorf(
			"can't lead round: %d because qbft Instance is currently at round: %d",
			newRound,
			i.State.Round,
		)
	}
	if i.State.ProposalAcceptedForCurrentRound != nil {
		return fmt.Errorf(
			"can't lead round: %d because already have accepted proposal for round: %d",
			newRound,
			i.State.Round,
		)
	}

	return nil
}

func (i *Instance) validRoundChangeForDataIgnoreSignature(
	msg *specqbft.ProcessingMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {
	if msg.QBFTMessage.MsgType != specqbft.RoundChangeMsgType {
		return errors.New("round change msg type is wrong")
	}
	if msg.QBFTMessage.Height != height {
		return errors.New("wrong msg height")
	}
	if msg.QBFTMessage.Round != round {
		return errors.New("wrong msg round")
	}
	if len(msg.SignedMessage.OperatorIDs) != 1 {
		return errors.New("msg allows 1 signer")
	}

	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "roundChange invalid")
	}

	if !msg.SignedMessage.CheckSignersInCommittee(i.State.CommitteeMember.Committee) {
		return errors.New("signer not in committee")
	}

	// Addition to formal spec
	// We add this extra tests on the msg itself to filter round change msgs with invalid justifications, before they are inserted into msg containers
	if msg.QBFTMessage.RoundChangePrepared() {
		r, err := specqbft.HashDataRoot(fullData)
		if err != nil {
			return errors.Wrap(err, "could not hash input data")
		}
		if !bytes.Equal(r[:], msg.QBFTMessage.Root[:]) {
			return errors.New("H(data) != root")
		}

		if msg.QBFTMessage.DataRound > round {
			return errors.New("prepared round > round")
		}

		// validate prepare message justifications

		// no need to check error, checked on msg.QBFTMessage.Validate()
		roundChangeJustifications, _ := msg.QBFTMessage.GetRoundChangeJustifications()
		roundChangeJustificationsWrapped := make([]*specqbft.ProcessingMessage, 0, len(roundChangeJustifications))
		for _, signedMessage := range roundChangeJustifications {
			procMsg, err := specqbft.NewProcessingMessage(signedMessage)
			if err != nil {
				return fmt.Errorf("%w: %s", convertJustificationToProcessingMsgErr, err)
			}
			roundChangeJustificationsWrapped = append(roundChangeJustificationsWrapped, procMsg)
		}
		for _, justification := range roundChangeJustificationsWrapped {
			if err := validSignedPrepareForHeightRoundAndRootVerifySignature(
				justification,
				i.State.Height,
				msg.QBFTMessage.DataRound,
				msg.QBFTMessage.Root,
				i.State.CommitteeMember.Committee); err != nil {
				return errors.Wrap(err, "round change justification invalid")
			}
		}
		if !specqbft.HasQuorum(i.State.CommitteeMember, roundChangeJustificationsWrapped) {
			return errors.New("no justifications quorum")
		}

		return nil
	}

	return nil
}

func (i *Instance) validRoundChangeForDataVerifySignature(
	msg *specqbft.ProcessingMessage,
	height specqbft.Height,
	round specqbft.Round,
	fullData []byte,
) error {
	if err := i.validRoundChangeForDataIgnoreSignature(msg, height, round, fullData); err != nil {
		return err
	}

	// Verify signature
	if err := spectypes.Verify(msg.SignedMessage, i.State.CommitteeMember.Committee); err != nil {
		return errors.Wrap(err, "msg signature invalid")
	}

	return nil
}

// highestPrepared returns a round change message with the highest prepared round, returns nil if none found
func highestPrepared(roundChanges []*specqbft.ProcessingMessage) (*specqbft.ProcessingMessage, error) {
	var ret *specqbft.ProcessingMessage
	var highestPreparedRound specqbft.Round
	for _, rc := range roundChanges {
		if !rc.QBFTMessage.RoundChangePrepared() {
			continue
		}

		if ret == nil {
			ret = rc
			highestPreparedRound = rc.QBFTMessage.DataRound
		} else {
			if highestPreparedRound < rc.QBFTMessage.DataRound {
				ret = rc
				highestPreparedRound = rc.QBFTMessage.DataRound
			}
		}
	}
	return ret, nil
}

// returns the min round number out of the signed round change messages and the current round
func minRound(roundChangeMsgs []*specqbft.ProcessingMessage) specqbft.Round {
	ret := specqbft.NoRound
	for _, msg := range roundChangeMsgs {
		if ret == specqbft.NoRound || ret > msg.QBFTMessage.Round {
			ret = msg.QBFTMessage.Round
		}
	}
	return ret
}

func (i *Instance) getRoundChangeData() (specqbft.Round, [32]byte, []byte, []*specqbft.ProcessingMessage, error) {
	if i.State.LastPreparedRound != specqbft.NoRound && i.State.LastPreparedValue != nil {
		justifications, err := i.getRoundChangeJustification(i.State.PrepareContainer)
		if err != nil {
			return specqbft.NoRound, [32]byte{}, nil, nil, errors.Wrap(err, "could not get round change justification")
		}

		r, err := specqbft.HashDataRoot(i.State.LastPreparedValue)
		if err != nil {
			return specqbft.NoRound, [32]byte{}, nil, nil, errors.Wrap(err, "could not hash input data")
		}

		return i.State.LastPreparedRound, r, i.State.LastPreparedValue, justifications, nil
	}
	return specqbft.NoRound, [32]byte{}, nil, nil, nil
}

// getRoundChangeJustification returns the round change justification for the current round.
// The justification is a quorum of signed prepare messages that agree on state.LastPreparedValue
func (i *Instance) getRoundChangeJustification(prepareMsgContainer *specqbft.MsgContainer) ([]*specqbft.ProcessingMessage, error) {
	if i.State.LastPreparedValue == nil {
		return nil, nil
	}

	r, err := specqbft.HashDataRoot(i.State.LastPreparedValue)
	if err != nil {
		return nil, errors.Wrap(err, "could not hash input data")
	}

	prepareMsgs := prepareMsgContainer.MessagesForRound(i.State.LastPreparedRound)
	ret := make([]*specqbft.ProcessingMessage, 0)
	for _, msg := range prepareMsgs {
		if err := validSignedPrepareForHeightRoundAndRootIgnoreSignature(
			msg,
			i.State.Height,
			i.State.LastPreparedRound,
			r,
			i.State.CommitteeMember.Committee,
		); err == nil {
			ret = append(ret, msg)
		}
	}

	if !specqbft.HasQuorum(i.State.CommitteeMember, ret) {
		return nil, nil
	}

	return ret, nil
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
func (i *Instance) CreateRoundChange(signer ssvtypes.OperatorSigner, newRound specqbft.Round, instanceStartValue []byte) (*spectypes.SignedSSVMessage, error) {
	lastPreparedRound, root, lastPreparedValue, justifications, err := i.getRoundChangeData()
	if err != nil {
		return nil, errors.Wrap(err, "could not generate round change data")
	}

	justificationsSignedMessages := make([]*spectypes.SignedSSVMessage, 0)
	for _, msg := range justifications {
		justificationsSignedMessages = append(justificationsSignedMessages, msg.SignedMessage)
	}

	justificationsData, err := specqbft.MarshalJustifications(justificationsSignedMessages)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal justifications")
	}
	msg := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     i.State.Height,
		Round:      newRound,
		Identifier: i.State.ID,

		Root:                     root,
		DataRound:                lastPreparedRound,
		RoundChangeJustification: justificationsData,
	}

	signedMsg, err := ssvtypes.Sign(msg, i.State.CommitteeMember.OperatorID, signer)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign round change message")
	}
	signedMsg.FullData = lastPreparedValue
	return signedMsg, nil
}
