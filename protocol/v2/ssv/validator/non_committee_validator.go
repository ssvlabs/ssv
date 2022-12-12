package validator

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/ibft/storage"
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
	config := &types.Config{
		Domain:  types.GetDefaultDomain(),
		Storage: opts.Storage.Get(identifier.GetRoleType()),
	}
	ctrl := qbftcontroller.NewController(identifier[:], &opts.SSVShare.Share, types.GetDefaultDomain(), config)
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
		logger.Debug("invalid message", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		signedMsg := &specqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			logger.Debug("could not get consensus Message from network Message", zap.Error(err))
			return
		}
		// only supports decided msg's
		if signedMsg.Message.MsgType != specqbft.CommitMsgType || !ncv.Share.HasQuorum(len(signedMsg.Signers)) {
			return
		}

		if decided, err := ncv.qbftController.ProcessMsg(signedMsg); err != nil {
			logger.Debug("failed to process message", zap.Error(err))
		} else if decided != nil {
			if inst := ncv.qbftController.StoredInstances.FindInstance(signedMsg.Message.Height); inst != nil {
				if err = ncv.qbftController.SaveHighestInstance(inst, signedMsg); err != nil {
					ncv.logger.Debug("failed to save instance", zap.Error(err))
				}
			}
		}
		return
	}
}
