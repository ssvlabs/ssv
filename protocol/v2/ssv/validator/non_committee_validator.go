package validator

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type NonCommitteeValidator struct {
	logger         *zap.Logger
	Share          *types.SSVShare
	Storage        *storage.QBFTStores
	qbftController *qbftcontroller.Controller
}

func NewNonCommitteeValidator(identifier spectypes.MessageID, opts Options) *NonCommitteeValidator {
	logger := logger.With(zap.String("who", "NonCommitteeValidator"),
		zap.String("identifier", identifier.String()))

	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:  types.GetDefaultDomain(),
		Storage: opts.Storage.Get(identifier.GetRoleType()),
		Network: opts.Network,
	}
	ctrl := qbftcontroller.NewController(identifier[:], &opts.SSVShare.Share, types.GetDefaultDomain(), config, opts.FullNode)
	ctrl.NewDecidedHandler = opts.NewDecidedHandler
	if err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		logger.Debug("failed to load highest instance", zap.Error(err))
	}

	return &NonCommitteeValidator{
		logger:         logger,
		Share:          opts.SSVShare,
		Storage:        opts.Storage,
		qbftController: ctrl,
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(msg *spectypes.SSVMessage) {
	logger := ncv.logger.With(zap.String("id", msg.GetID().String()))
	if err := validateMessage(ncv.Share.Share, msg); err != nil {
		logger.Debug("got invalid message", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		signedMsg := &specqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			logger.Debug("failed to get consensus Message from network Message", zap.Error(err))
			return
		}
		if signedMsg == nil || signedMsg.Message == nil {
			logger.Debug("got empty message")
			return
		}
		// only supports decided msg's
		if signedMsg.Message.MsgType != specqbft.CommitMsgType || !ncv.Share.HasQuorum(len(signedMsg.Signers)) {
			return
		}

		if decided, err := ncv.qbftController.ProcessMsg(signedMsg); err != nil {
			logger.Debug("failed to process message",
				zap.Uint64("msg_height", uint64(signedMsg.Message.Height)),
				zap.Any("signers", signedMsg.Signers),
				zap.Error(err))
		} else if decided != nil {
			if inst := ncv.qbftController.StoredInstances.FindInstance(signedMsg.Message.Height); inst != nil {
				logger := logger.With(
					zap.Uint64("msg_height", uint64(signedMsg.Message.Height)),
					zap.Uint64("ctrl_height", uint64(ncv.qbftController.Height)),
					zap.Any("signers", signedMsg.Signers),
				)
				if err = ncv.qbftController.SaveInstance(inst, signedMsg); err != nil {
					logger.Debug("failed to save instance", zap.Error(err))
				} else {
					logger.Debug("saved instance")
				}
			}
		}
		return
	}
}
