package instance

import (
	"bytes"
	"context"

	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// uponRoundChange process round change messages.
// Assumes round change message is valid!
func (i *Instance) uponRoundChange(
	ctx context.Context,
	logger *zap.Logger,
	msg *specqbft.ProcessingMessage,
) error {
	hasQuorumBefore := specqbft.HasQuorum(i.State.CommitteeMember, i.State.RoundChangeContainer.MessagesForRound(msg.QBFTMessage.Round))
	// Currently, even if we have a quorum of round change messages, we update the container
	addedMsg, err := i.State.RoundChangeContainer.AddFirstMsgForSignerAndRound(msg)
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
		zap.Uint64("qbft_instance_round", uint64(i.State.Round)),
		zap.Uint64("qbft_instance_height", uint64(i.State.Height)),
	)

	logger.Debug("🔄 got round change",
		fields.Root(msg.QBFTMessage.Root),
		zap.Any("round_change_signers", msg.SignedMessage.OperatorIDs),
	)

	justifiedRoundChangeMsg, valueToPropose, err := i.hasReceivedProposalJustificationForLeadingRound(msg)
	if err != nil {
		return errors.Wrap(err, "could not get proposal justification for leading round")
	}

	if justifiedRoundChangeMsg != nil {
		roundChangeJustificationSignedMessages, _ := justifiedRoundChangeMsg.QBFTMessage.GetRoundChangeJustifications() // no need to check error, check on isValidRoundChange

		roundChangeJustification := make([]*specqbft.ProcessingMessage, 0)
		for _, rcSignedMessage := range roundChangeJustificationSignedMessages {
			rc, err := specqbft.NewProcessingMessage(rcSignedMessage)
			if err != nil {
				return errors.Wrap(err, "could not create ProcessingMessage from round change justification")
			}
			roundChangeJustification = append(roundChangeJustification, rc)
		}

		proposal, err := i.CreateProposal(
			valueToPropose,
			i.State.RoundChangeContainer.MessagesForRound(i.State.Round), // TODO - might be optimized to include only necessary quorum
			roundChangeJustification,
		)
		if err != nil {
			return errors.Wrap(err, "failed to create proposal")
		}

		r, _ := specqbft.HashDataRoot(valueToPropose) // TODO: err check although already happens in createproposal

		i.metrics.RecordRoundChange(ctx, msg.QBFTMessage.Round, reasonJustified)

		logger.Debug("🔄 got justified round change, broadcasting proposal message",
			zap.Any("round_change_signers", allSigners(i.State.RoundChangeContainer.MessagesForRound(i.State.Round))),
			fields.Root(r),
		)

		if err := i.Broadcast(proposal); err != nil {
			return errors.Wrap(err, "failed to broadcast proposal message")
		}
	} else if partialQuorum, rcs := i.hasReceivedPartialQuorum(); partialQuorum {
		newRound := minRound(rcs)
		if newRound <= i.State.Round {
			return nil // no need to advance round
		}

		i.metrics.RecordRoundChange(ctx, newRound, reasonPartialQuorum)

		err := i.uponChangeRoundPartialQuorum(logger, newRound)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) uponChangeRoundPartialQuorum(logger *zap.Logger, newRound specqbft.Round) error {
	i.bumpToRound(newRound)
	i.State.ProposalAcceptedForCurrentRound = nil

	i.config.GetTimer().TimeoutForRound(i.State.Height, i.State.Round)

	roundChange, err := i.CreateRoundChange(newRound)
	if err != nil {
		return errors.Wrap(err, "failed to create round change message")
	}

	root, err := specqbft.HashDataRoot(i.StartValue)
	if err != nil {
		return errors.Wrap(err, "failed to hash instance start value")
	}

	logger.Debug("📢 got partial quorum, broadcasting round change message",
		zap.Uint64("qbft_new_round", uint64(newRound)),
		fields.Root(root),
		zap.Any("round_change_signers", roundChange.OperatorIDs),
	)

	if err := i.Broadcast(roundChange); err != nil {
		return errors.Wrap(err, "failed to broadcast round change message")
	}

	return nil
}

func (i *Instance) hasReceivedPartialQuorum() (bool, []*specqbft.ProcessingMessage) {
	all := i.State.RoundChangeContainer.AllMessages()

	rc := make([]*specqbft.ProcessingMessage, 0)
	for _, msg := range all {
		if msg.QBFTMessage.Round > i.State.Round {
			rc = append(rc, msg)
		}
	}

	return specqbft.HasPartialQuorum(i.State.CommitteeMember, rc), rc
}

// hasReceivedProposalJustificationForLeadingRound (if operator is a leader for the round):
//   - if first round or not received round change msgs with prepare justification
//     returns first round change msg in container and value to propose
//   - if received round change msgs with prepare justification returns the highest
//     prepare justification round change msg and value to propose
//
// If operator is not a leader for the round - return nil, nil, nil.
func (i *Instance) hasReceivedProposalJustificationForLeadingRound(
	roundChangeMessage *specqbft.ProcessingMessage,
) (*specqbft.ProcessingMessage, []byte, error) {
	roundChanges := i.State.RoundChangeContainer.MessagesForRound(roundChangeMessage.QBFTMessage.Round)
	// optimization, if no round change quorum can return false
	if !specqbft.HasQuorum(i.State.CommitteeMember, roundChanges) {
		return nil, nil, nil
	}

	// Important!
	// We iterate on all round change msgs for liveliness in case the last round change msg is malicious.
	for _, containerRoundChangeMessage := range roundChanges {
		// Chose proposal value.
		// If justifiedRoundChangeMsg has no prepare justification chose state value
		// If justifiedRoundChangeMsg has prepare justification chose prepared value
		valueToPropose := i.StartValue
		if containerRoundChangeMessage.QBFTMessage.RoundChangePrepared() {
			valueToPropose = containerRoundChangeMessage.SignedMessage.FullData
		}

		roundChangeSignedMessagesJustification, _ := containerRoundChangeMessage.QBFTMessage.GetRoundChangeJustifications() // no need to check error, checked on isValidRoundChange

		roundChangeJustification := make([]*specqbft.ProcessingMessage, 0)
		for _, signedMessage := range roundChangeSignedMessagesJustification {
			msg, err := specqbft.NewProcessingMessage(signedMessage)
			if err != nil {
				return nil, nil, errors.Wrap(err, "could not create ProcessingMessage from round change justification")
			}
			roundChangeJustification = append(roundChangeJustification, msg)
		}

		if i.isProposalJustificationForLeadingRound(
			containerRoundChangeMessage,
			roundChanges,
			roundChangeJustification,
			valueToPropose,
			roundChangeMessage.QBFTMessage.Round,
		) == nil {
			// not returning error, no need to
			return containerRoundChangeMessage, valueToPropose, nil
		}
	}
	return nil, nil, nil
}

// isProposalJustificationForLeadingRound - returns nil if we have a quorum of round change msgs and highest justified value for leading round
func (i *Instance) isProposalJustificationForLeadingRound(
	roundChangeMsg *specqbft.ProcessingMessage,
	roundChanges []*specqbft.ProcessingMessage,
	roundChangeJustifications []*specqbft.ProcessingMessage,
	value []byte,
	newRound specqbft.Round,
) error {
	if err := i.isReceivedProposalJustification(
		roundChanges,
		roundChangeJustifications,
		roundChangeMsg.QBFTMessage.Round,
		value,
	); err != nil {
		return err
	}

	if i.proposer(roundChangeMsg.QBFTMessage.Round) != i.State.CommitteeMember.OperatorID {
		return errors.New("not proposer")
	}

	currentRoundProposal := i.State.ProposalAcceptedForCurrentRound == nil && i.State.Round == newRound
	futureRoundProposal := newRound > i.State.Round

	if !currentRoundProposal && !futureRoundProposal {
		return errors.New("proposal round mismatch")
	}

	return nil
}

// isReceivedProposalJustification - returns nil if we have a quorum of round change msgs and highest justified value
func (i *Instance) isReceivedProposalJustification(
	roundChanges, prepares []*specqbft.ProcessingMessage,
	newRound specqbft.Round,
	value []byte,
) error {
	if err := i.isProposalJustification(
		roundChanges,
		prepares,
		newRound,
		value,
	); err != nil {
		return errors.Wrap(err, "proposal not justified")
	}
	return nil
}

func (i *Instance) validRoundChangeForDataIgnoreSignature(
	msg *specqbft.ProcessingMessage,
	round specqbft.Round,
	fullData []byte,
) error {
	if msg.QBFTMessage.MsgType != specqbft.RoundChangeMsgType {
		return errors.New("round change msg type is wrong")
	}
	if msg.QBFTMessage.Height != i.State.Height {
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

		// validate prepare message justifications
		prepareSignedMsgs, _ := msg.QBFTMessage.GetRoundChangeJustifications() // no need to check error, checked on msg.QBFTMessage.Validate()

		prepareMsgs := make([]*specqbft.ProcessingMessage, 0)
		for _, signedMessage := range prepareSignedMsgs {
			procMsg, err := specqbft.NewProcessingMessage(signedMessage)
			if err != nil {
				return errors.Wrap(err, "could not create ProcessingMessage from prepare message in round change justification")
			}
			prepareMsgs = append(prepareMsgs, procMsg)
		}

		for _, pm := range prepareMsgs {
			if err := i.validSignedPrepareForHeightRoundAndRootVerifySignature(
				pm,
				msg.QBFTMessage.DataRound,
				msg.QBFTMessage.Root,
			); err != nil {
				return errors.Wrap(err, "round change justification invalid")
			}
		}

		if !bytes.Equal(r[:], msg.QBFTMessage.Root[:]) {
			return errors.New("H(data) != root")
		}

		if !specqbft.HasQuorum(i.State.CommitteeMember, prepareMsgs) {
			return errors.New("no justifications quorum")
		}

		if msg.QBFTMessage.DataRound > round {
			return errors.New("prepared round > round")
		}

		return nil
	}

	return nil
}

func (i *Instance) validRoundChangeForDataVerifySignature(
	msg *specqbft.ProcessingMessage,
	round specqbft.Round,
	fullData []byte,
) error {
	if err := i.validRoundChangeForDataIgnoreSignature(msg, round, fullData); err != nil {
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

// returns the min round number out of the signed round change messages
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
		justifications, err := i.getRoundChangeJustification()
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
func (i *Instance) CreateRoundChange(newRound specqbft.Round) (*spectypes.SignedSSVMessage, error) {
	round, root, fullData, justifications, err := i.getRoundChangeData()
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
		DataRound:                round,
		RoundChangeJustification: justificationsData,
	}

	signedMsg, err := ssvtypes.Sign(msg, i.State.CommitteeMember.OperatorID, i.signer)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign round change message")
	}
	signedMsg.FullData = fullData
	return signedMsg, nil
}
