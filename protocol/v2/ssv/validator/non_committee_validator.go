package validator

import (
	"encoding/hex"
	"fmt"
	"sort"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type NonCommitteeValidator struct {
	Share              *types.SSVShare
	Storage            *storage.QBFTStores
	qbftController     *qbftcontroller.Controller
	commitMsgContainer *specqbft.MsgContainer
}

func NewNonCommitteeValidator(logger *zap.Logger, identifier spectypes.MessageID, opts Options) *NonCommitteeValidator {
	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:                types.GetDefaultDomain(),
		Storage:               opts.Storage.Get(identifier.GetRoleType()),
		Network:               opts.Network,
		SignatureVerification: true,
	}
	ctrl := qbftcontroller.NewController(identifier[:], &opts.SSVShare.Share, config, opts.FullNode)
	ctrl.StoredInstances = make(qbftcontroller.InstanceContainer, 0, nonCommitteeInstanceContainerCapacity(opts.FullNode))
	ctrl.NewDecidedHandler = opts.NewDecidedHandler
	if _, err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		logger.Debug("❗ failed to load highest instance", zap.Error(err))
	}

	return &NonCommitteeValidator{
		Share:              opts.SSVShare,
		Storage:            opts.Storage,
		qbftController:     ctrl,
		commitMsgContainer: specqbft.NewMsgContainer(),
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) {
	logger = logger.With(fields.PubKey(msg.MsgID.GetPubKey()), fields.Role(msg.MsgID.GetRoleType()))

	if err := validateMessage(ncv.Share.Share, msg); err != nil {
		logger.Debug("❌ got invalid message", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		signedMsg := &specqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			logger.Debug("❗ failed to get consensus Message from network Message", zap.Error(err))
			return
		}
		// only supports commit msg's
		if signedMsg.Message.MsgType != specqbft.CommitMsgType {
			return
		}

		logger = logger.With(fields.Height(signedMsg.Message.Height))

		go func() {
			watchedPKList := []string{
				"857400e153569ee54a202b92ef1223c00b1d39891c93aafdd776698ae9b2d3d54c6319763d9bb0ecf72e06cb3c16724d",
				"98ee39df56331d7e10c23a26024b5054b6e64c490fb02a463a0f925b873cfbd5f230e2472fbcd266f7012e26fa3ceb68",
			}

			if !slices.Contains(watchedPKList, hex.EncodeToString(msg.MsgID.GetPubKey())) {
				logger.Debug("ncv ignoring pk")
				return
			}

			logger.Debug("ncv processing pk")

			addMsg, err := ncv.commitMsgContainer.AddFirstMsgForSignerAndRound(signedMsg)
			if err != nil {
				logger.Debug("❌ could not add commit msg to container",
					zap.Uint64("msg_height", uint64(signedMsg.Message.Height)),
					zap.Any("signers", signedMsg.Signers),
					zap.Error(err))
				return
			}
			if !addMsg {
				return
			}

			signers, commitMsgs := ncv.commitMsgContainer.LongestUniqueSignersForRoundAndRoot(signedMsg.Message.Round, signedMsg.Message.Root)
			if !ncv.Share.HasQuorum(len(signers)) {
				return
			}

			signedMsg, err = aggregateCommitMsgs(commitMsgs)
			if err != nil {
				logger.Debug("❌ could not add aggregate commit messages",
					zap.Uint64("msg_height", uint64(signedMsg.Message.Height)),
					zap.Any("signers", signedMsg.Signers),
					zap.Error(err))
				return
			}

			if _, err := ncv.UponDecided(logger, signedMsg); err != nil {
				logger.Debug("❌ failed to process message",
					zap.Uint64("msg_height", uint64(signedMsg.Message.Height)),
					zap.Any("signers", signedMsg.Signers),
					zap.Error(err))
				return
			}
		}()
	}
}

// nonCommitteeInstanceContainerCapacity returns the capacity of InstanceContainer for non-committee validators
func nonCommitteeInstanceContainerCapacity(fullNode bool) int {
	if fullNode {
		// Helps full nodes reduce
		return 2
	}
	return 1
}

func aggregateCommitMsgs(msgs []*specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("can't aggregate zero commit msgs")
	}

	var ret *specqbft.SignedMessage
	for _, m := range msgs {
		if ret == nil {
			ret = m.DeepCopy()
		} else {
			if err := ret.Aggregate(m); err != nil {
				return nil, fmt.Errorf("could not aggregate commit msg: %w", err)
			}
		}
	}

	sort.Slice(ret.Signers, func(i, j int) bool {
		return ret.Signers[i] < ret.Signers[j]
	})

	return ret, nil
}

// UponDecided returns decided msg if decided, nil otherwise
func (ncv *NonCommitteeValidator) UponDecided(logger *zap.Logger, msg *specqbft.SignedMessage) (*specqbft.SignedMessage, error) {
	// try to find instance
	inst := ncv.qbftController.InstanceForHeight(logger, msg.Message.Height)
	prevDecided := inst != nil && inst.State.Decided
	isFutureDecided := msg.Message.Height > ncv.qbftController.Height
	save := true

	if inst == nil {
		i := instance.NewInstance(ncv.qbftController.GetConfig(), ncv.qbftController.Share, ncv.qbftController.Identifier, msg.Message.Height)
		i.State.Round = msg.Message.Round
		i.State.Decided = true
		i.State.DecidedValue = msg.FullData
		i.State.CommitContainer.AddMsg(msg)
		ncv.qbftController.StoredInstances.AddNewInstance(i)
	} else if decided, _ := inst.IsDecided(); !decided {
		inst.State.Decided = true
		inst.State.Round = msg.Message.Round
		inst.State.DecidedValue = msg.FullData
		inst.State.CommitContainer.AddMsg(msg)
	} else { // decide previously, add if has more signers
		signers, _ := inst.State.CommitContainer.LongestUniqueSignersForRoundAndRoot(msg.Message.Round, msg.Message.Root)
		if len(msg.Signers) > len(signers) {
			inst.State.CommitContainer.AddMsg(msg)
		} else {
			save = false
		}
	}

	if save {
		// Retrieve instance from StoredInstances (in case it was created above)
		// and save it together with the decided message.
		if inst := ncv.qbftController.StoredInstances.FindInstance(msg.Message.Height); inst != nil {
			logger := logger.With(
				zap.Uint64("msg_height", uint64(msg.Message.Height)),
				zap.Uint64("ctrl_height", uint64(ncv.qbftController.Height)),
				zap.Any("signers", msg.Signers),
			)
			if err := ncv.qbftController.SaveInstance(inst, msg); err != nil {
				logger.Debug("❗failed to save instance", zap.Error(err))
			} else {
				logger.Debug("💾 saved instance upon decided", zap.Error(err))
			}
		}
	}

	if isFutureDecided {
		// bump height
		ncv.qbftController.Height = msg.Message.Height
	}
	if ncv.qbftController.NewDecidedHandler != nil {
		ncv.qbftController.NewDecidedHandler(msg)
	}
	if !prevDecided {
		return msg, nil
	}
	return nil, nil
}
