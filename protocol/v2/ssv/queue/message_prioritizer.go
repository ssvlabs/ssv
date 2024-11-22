package queue

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/logging/fields"
	"go.uber.org/zap"
)

// State represents a portion of the current state
// that is relevant to the prioritization of messages.
type State struct {
	HasRunningInstance bool
	Height             qbft.Height
	Round              qbft.Round
	Slot               phase0.Slot
	Quorum             uint64
}

// MessagePrioritizer is an interface for prioritizing messages.
type MessagePrioritizer interface {
	// Prior returns true if message A should be prioritized over B.
	Prior(a, b *SSVMessage) bool
}

type standardPrioritizer struct {
	state *State
}

// NewMessagePrioritizer returns a standard implementation for MessagePrioritizer
// which prioritizes messages according to the given State.
func NewMessagePrioritizer(state *State) MessagePrioritizer {
	return &standardPrioritizer{state: state}
}

func (p *standardPrioritizer) Prior(a, b *SSVMessage) bool {
	msgScoreA, msgScoreB := scoreMessageType(a), scoreMessageType(b)
	if msgScoreA != msgScoreB {
		return msgScoreA > msgScoreB
	}

	relativeHeightA, relativeHeightB := compareHeightOrSlot(p.state, a), compareHeightOrSlot(p.state, b)
	if relativeHeightA != relativeHeightB {
		return scoreHeight(relativeHeightA) > scoreHeight(relativeHeightB)
	}

	scoreA, scoreB := scoreMessageSubtype(p.state, a, relativeHeightA), scoreMessageSubtype(p.state, b, relativeHeightB)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = scoreRound(p.state, a), scoreRound(p.state, b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	scoreA, scoreB = scoreConsensusType(a), scoreConsensusType(b)
	if scoreA != scoreB {
		return scoreA > scoreB
	}

	return true
}

func scoreHeight(relativeHeight int) int {
	switch relativeHeight {
	case 0:
		return 2
	case 1:
		return 1
	case -1:
		return 0
	}
	return 0
}

func NewCommitteeQueuePrioritizer(logger *zap.Logger, state *State) MessagePrioritizer {
	return &committeePrioritizer{logger: logger, state: state}
}

type committeePrioritizer struct {
	state  *State
	logger *zap.Logger
}

func (p *committeePrioritizer) Prior(a, b *SSVMessage) (ok bool) {
	var fields []zap.Field

	defer func() {
		fields = append(fields, zap.Bool("result", ok))
		logMsg(p.logger, a, "resultonly", fields...)
	}()

	logMsg(p.logger, a, "messageA")
	logMsg(p.logger, b, "messageB")

	msgScoreA, msgScoreB := scoreMessageType(a), scoreMessageType(b)
	if msgScoreA != msgScoreB {
		fields = append(fields, zap.String("check", "msgScoreA > msgScoreB"))
		return msgScoreA > msgScoreB
	}

	relativeHeightA, relativeHeightB := compareHeightOrSlot(p.state, a), compareHeightOrSlot(p.state, b)
	if relativeHeightA != relativeHeightB {
		fields = append(fields, zap.String("check", "scoreHeight(relativeHeightA) > scoreHeight(relativeHeightB)"))
		return scoreHeight(relativeHeightA) > scoreHeight(relativeHeightB)
	}

	scoreA, scoreB := scoreCommitteeMessageSubtype(p.state, a, relativeHeightA), scoreCommitteeMessageSubtype(p.state, b, relativeHeightB)
	if scoreA != scoreB {
		fields = append(fields, zap.String("check", "1#scoreA > scoreB"))
		return scoreA > scoreB
	}

	scoreA, scoreB = scoreConsensusType(a), scoreConsensusType(b)
	if scoreA != scoreB {
		fields = append(fields, zap.String("check", "2#scoreA > scoreB"))
		return scoreA > scoreB
	}

	fields = append(fields, zap.String("check", "return true"))
	return true
}

func logMsg(logger *zap.Logger, msg *SSVMessage, logMsg string, lf ...zap.Field) {
	lf = append(lf, fields.Role(msg.MsgID.GetRoleType()))

	cid := spectypes.CommitteeID(msg.GetID().GetDutyExecutorID()[16:])
	lf = append(lf, fields.CommitteeID(cid))

	switch msg.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		sm := msg.Body.(*specqbft.Message)
		lf = append(lf, fields.QBFTMessageType(sm.MsgType))
		lf = append(lf, zap.Uint64("msg_height", uint64(sm.Height)))
	case spectypes.SSVPartialSignatureMsgType:
		psm := msg.Body.(*spectypes.PartialSignatureMessages)
		lf = append(lf, fields.MessageType(msg.MsgType))
		lf = append(lf, zap.Uint64("signer", psm.Messages[0].Signer))
		lf = append(lf, fields.Slot(psm.Slot))
	}
	logger.Debug(logMsg, lf...)
}
