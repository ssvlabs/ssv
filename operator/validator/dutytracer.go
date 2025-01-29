package validator

import (
	"encoding/hex"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	model "github.com/ssvlabs/ssv/exporter/v2"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

type InMemTracer struct {
	logger          *zap.Logger
	validatorTraces map[uint64]*model.ValidatorDutyTrace
	mu              sync.Mutex
}

func NewTracer(logger *zap.Logger) *InMemTracer {
	return &InMemTracer{
		logger:          logger,
		validatorTraces: make(map[uint64]*model.ValidatorDutyTrace),
	}
}

func (n *InMemTracer) qbft(msg *specqbft.Message) {
	slot := uint64(msg.Height)

	fields := []zap.Field{
		fields.Slot(phase0.Slot(slot)),
		fields.Round(msg.Round),
		zap.String("identifier", hex.EncodeToString(msg.Identifier[len(msg.Identifier)-5:])),
		zap.String("root", hex.EncodeToString(msg.Root[len(msg.Root)-5:])),
	}

	n.mu.Lock()
	trace, ok := n.validatorTraces[slot]
	if !ok {
		trace = new(model.ValidatorDutyTrace)
		n.validatorTraces[slot] = trace
		fields = append(fields, zap.String("new Trace", "true"))
	}
	n.mu.Unlock()

	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		fields = append(fields, zap.String("messageType", "proposal"))
	case specqbft.PrepareMsgType:
		fields = append(fields, zap.String("messageType", "prepare"))
	case specqbft.CommitMsgType:
		fields = append(fields, zap.String("messageType", "commit"))
	case specqbft.RoundChangeMsgType:
		fields = append(fields, zap.String("messageType", "round change"))
	}

	var lastRound *model.RoundTrace

	currentRound := specqbft.Round(len(trace.Rounds))
	if currentRound == 0 || currentRound > msg.Round {
		lastRound = &model.RoundTrace{}
		trace.Rounds = append(trace.Rounds, lastRound)
		fields = append(fields, zap.String("new Round", "true"))
	} else {
		lastRound = trace.Rounds[len(trace.Rounds)-1]
	}

	fields = append(fields, zap.Int("duty rounds", len(trace.Rounds)))

	lastRound.Proposer = 0 //fixme
	lastRound.ProposalRoot = msg.Root
	// lastRound.ProposalReceivedTime = uint64(time.Now().Unix())

	n.logger.Info("qbft", fields...)

	_ = msg.DataRound
	_ = msg.RoundChangeJustification
	_ = msg.PrepareJustification

	// trace the message
	// n.validatorTraces = append(n.validatorTraces, model.ValidatorDutyTrace{})
}

func (n *InMemTracer) signed(msg *spectypes.PartialSignatureMessages) {
	slot := uint64(msg.Slot)

	fields := []zap.Field{
		fields.Slot(phase0.Slot(slot)),
	}

	n.mu.Lock()
	trace, ok := n.validatorTraces[slot]
	if !ok {
		trace = new(model.ValidatorDutyTrace)
		n.validatorTraces[slot] = trace
		fields = append(fields, zap.String("new Trace", "true"))
	}
	n.mu.Unlock()

	fields = append(fields, zap.Int("messages", len(msg.Messages)))
	fields = append(fields, zap.Int("duty rounds", len(trace.Rounds)))

	lastRound := trace.Rounds[len(trace.Rounds)-1]
	for _, pSigMsg := range msg.Messages {
		lastRound.Proposer = pSigMsg.Signer
		lastRound.ProposalRoot = pSigMsg.SigningRoot
		// lastRound.ProposalReceivedTime = uint64(time.Now().Unix())

		_ = pSigMsg.ValidatorIndex
	}

	switch msg.Type {
	case spectypes.PostConsensusPartialSig:
		fields = append(fields, zap.String("messageType", "post consensus"))
	case spectypes.ContributionProofs:
		fields = append(fields, zap.String("messageType", "contribution proofs"))
	case spectypes.RandaoPartialSig:
		fields = append(fields, zap.String("messageType", "randao"))
	case spectypes.SelectionProofPartialSig:
		fields = append(fields, zap.String("messageType", "selection proof"))
	case spectypes.ValidatorRegistrationPartialSig:
		fields = append(fields, zap.String("messageType", "validator registration"))
	case spectypes.VoluntaryExitPartialSig:
		fields = append(fields, zap.String("messageType", "voluntary exit"))
	}

	n.logger.Info("signed", fields...)
}

func (n *InMemTracer) Trace(msg *queue.SSVMessage) {
	switch msg.MsgType {
	case spectypes.SSVConsensusMsgType:
		if subMsg, ok := msg.Body.(*specqbft.Message); ok {
			n.qbft(subMsg)
		}
	case spectypes.SSVPartialSignatureMsgType:
		pSigMessages := new(spectypes.PartialSignatureMessages)
		signedMsg := msg.SignedSSVMessage
		ssvMsg := signedMsg.SSVMessage

		_ = signedMsg.OperatorIDs
		_ = signedMsg.Signatures

		err := pSigMessages.Decode(ssvMsg.GetData())
		if err == nil {
			n.signed(pSigMessages)
		}
	}
}
