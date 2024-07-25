package instance

import (
	"encoding/base64"
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
)

// Instance is a single QBFT instance that starts with a Start call (including a value).
// Every new msg the ProcessMsg function needs to be called
type Instance struct {
	State  *specqbft.State
	config qbft.IConfig

	processMsgF *spectypes.ThreadSafeF
	startOnce   sync.Once

	forceStop  bool
	StartValue []byte

	metrics *metrics
}

func NewInstance(
	config qbft.IConfig,
	committeeMember *spectypes.CommitteeMember,
	identifier []byte,
	height specqbft.Height,
) *Instance {
	var name string
	if len(identifier) == 56 {
		name = spectypes.MessageID(identifier).GetRoleType().String()
	} else {
		name = base64.StdEncoding.EncodeToString(identifier)
	}

	return &Instance{
		State: &specqbft.State{
			CommitteeMember:      committeeMember,
			ID:                   identifier,
			Round:                specqbft.FirstRound,
			Height:               height,
			LastPreparedRound:    specqbft.NoRound,
			ProposeContainer:     specqbft.NewMsgContainer(),
			PrepareContainer:     specqbft.NewMsgContainer(),
			CommitContainer:      specqbft.NewMsgContainer(),
			RoundChangeContainer: specqbft.NewMsgContainer(),
		},
		config:      config,
		processMsgF: spectypes.NewThreadSafeF(),
		metrics:     newMetrics(name),
	}
}

func (i *Instance) ForceStop() {
	i.forceStop = true
}

// Start is an interface implementation
func (i *Instance) Start(logger *zap.Logger, value []byte, height specqbft.Height) {
	i.startOnce.Do(func() {
		i.StartValue = value
		i.bumpToRound(specqbft.FirstRound)
		i.State.Height = height
		i.metrics.StartStage()
		i.config.GetTimer().TimeoutForRound(height, specqbft.FirstRound)

		logger = logger.With(
			fields.Round(i.State.Round),
			fields.Height(i.State.Height))

		proposerID := proposer(i.State, i.GetConfig(), specqbft.FirstRound)
		logger.Debug("ℹ️ starting QBFT instance", zap.Uint64("leader", proposerID))

		// propose if this node is the proposer
		if proposerID == i.State.CommitteeMember.OperatorID {
			proposal, err := CreateProposal(i.State, i.config, i.StartValue, nil, nil)
			// nolint
			if err != nil {
				logger.Warn("❗ failed to create proposal", zap.Error(err))
				// TODO align spec to add else to avoid broadcast errored proposal
			} else {

				r, err := specqbft.HashDataRoot(i.StartValue) // @TODO (better than decoding?)
				if err != nil {
					logger.Warn("❗ failed to hash input data", zap.Error(err))
					return
				}
				// nolint
				logger = logger.With(fields.Root(r))
				logger.Debug("📢 leader broadcasting proposal message")
				if err := i.Broadcast(logger, proposal); err != nil {
					logger.Warn("❌ failed to broadcast proposal", zap.Error(err))
				}
			}
		}
	})
}

func (i *Instance) Broadcast(logger *zap.Logger, msg *spectypes.SignedSSVMessage) error {
	if !i.CanProcessMessages() {
		return errors.New("instance stopped processing messages")
	}
	msgID := spectypes.MessageID{}
	copy(msgID[:], i.State.ID)

	return i.config.GetNetwork().Broadcast(msgID, msg)
}

func allSigners(all []*spectypes.SignedSSVMessage) []spectypes.OperatorID {
	signers := make([]spectypes.OperatorID, 0, len(all))
	for _, m := range all {
		signers = append(signers, m.OperatorIDs...)
	}
	return signers
}

// ProcessMsg processes a new QBFT msg, returns non nil error on msg processing error
func (i *Instance) ProcessMsg(logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) (decided bool, decidedValue []byte, aggregatedCommit *spectypes.SignedSSVMessage, err error) {
	if !i.CanProcessMessages() {
		return false, nil, nil, errors.New("instance stopped processing messages")
	}

	if err := i.BaseMsgValidation(signedMsg); err != nil {
		return false, nil, nil, errors.Wrap(err, "invalid signed message")
	}

	msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
	if err != nil {
		return false, nil, nil, err
	}

	res := i.processMsgF.Run(func() interface{} {

		switch msg.MsgType {
		case specqbft.ProposalMsgType:
			return i.uponProposal(logger, signedMsg, i.State.ProposeContainer)
		case specqbft.PrepareMsgType:
			return i.uponPrepare(logger, signedMsg, i.State.PrepareContainer)
		case specqbft.CommitMsgType:
			decided, decidedValue, aggregatedCommit, err = i.UponCommit(logger, signedMsg, i.State.CommitContainer)
			if decided {
				i.State.Decided = decided
				i.State.DecidedValue = decidedValue
			}
			return err
		case specqbft.RoundChangeMsgType:
			return i.uponRoundChange(logger, i.StartValue, signedMsg, i.State.RoundChangeContainer, i.config.GetValueCheckF())
		default:
			return errors.New("signed message type not supported")
		}
	})
	if res != nil {
		return false, nil, nil, res.(error)
	}
	return i.State.Decided, i.State.DecidedValue, aggregatedCommit, nil
}

func (i *Instance) BaseMsgValidation(signedMsg *spectypes.SignedSSVMessage) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "invalid SignedSSVMessage")
	}

	msg, err := specqbft.DecodeMessage(signedMsg.SSVMessage.Data)
	if err != nil {
		return err
	}

	if err := msg.Validate(); err != nil {
		return errors.Wrap(err, "invalid Message")
	}

	if msg.Round < i.State.Round {
		return errors.New("past round")
	}

	switch msg.MsgType {
	case specqbft.ProposalMsgType:
		return isValidProposal(
			i.State,
			i.config,
			signedMsg,
			i.config.GetValueCheckF(),
		)
	case specqbft.PrepareMsgType:
		proposedSignedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedSignedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}

		proposedMsg, err := specqbft.DecodeMessage(proposedSignedMsg.SSVMessage.Data)
		if err != nil {
			return errors.Wrap(err, "proposal saved for this round is invalid")
		}

		return validSignedPrepareForHeightRoundAndRootIgnoreSignature(
			signedMsg,
			i.State.Height,
			i.State.Round,
			proposedMsg.Root,
			i.State.CommitteeMember.Committee,
		)
	case specqbft.CommitMsgType:
		proposedMsg := i.State.ProposalAcceptedForCurrentRound
		if proposedMsg == nil {
			return errors.New("did not receive proposal for this round")
		}
		return validateCommit(
			signedMsg,
			i.State.Height,
			i.State.Round,
			i.State.ProposalAcceptedForCurrentRound,
			i.State.CommitteeMember.Committee,
		)
	case specqbft.RoundChangeMsgType:
		return validRoundChangeForDataIgnoreSignature(i.State, i.config, signedMsg, i.State.Height, msg.Round, signedMsg.FullData)
	default:
		return errors.New("signed message type not supported")
	}
}

// IsDecided interface implementation
func (i *Instance) IsDecided() (bool, []byte) {
	if state := i.State; state != nil {
		return state.Decided, state.DecidedValue
	}
	return false, nil
}

// GetConfig returns the instance config
func (i *Instance) GetConfig() qbft.IConfig {
	return i.config
}

// SetConfig returns the instance config
func (i *Instance) SetConfig(config qbft.IConfig) {
	i.config = config
}

// GetHeight interface implementation
func (i *Instance) GetHeight() specqbft.Height {
	return i.State.Height
}

// GetRoot returns the state's deterministic root
func (i *Instance) GetRoot() ([32]byte, error) {
	return i.State.GetRoot()
}

// Encode implementation
func (i *Instance) Encode() ([]byte, error) {
	return json.Marshal(i)
}

// Decode implementation
func (i *Instance) Decode(data []byte) error {
	return json.Unmarshal(data, &i)
}

// bumpToRound sets round and sends current round metrics.
func (i *Instance) bumpToRound(round specqbft.Round) {
	i.State.Round = round
	i.metrics.SetRound(round)
}

// CanProcessMessages will return true if instance can process messages
func (i *Instance) CanProcessMessages() bool {
	return !i.forceStop && int(i.State.Round) < CutoffRound
}
