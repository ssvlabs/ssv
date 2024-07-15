package validator

import (
	specqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/genesis/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

type NonCommitteeValidator struct {
	Share          *types.SSVShare
	Storage        *storage.QBFTStores
	qbftController *qbftcontroller.Controller
}

func (ncv *NonCommitteeValidator) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) {
	logger = logger.With(fields.PubKey(msg.MsgID.GetPubKey()), fields.BeaconRole(spectypes.BeaconRole(msg.MsgID.GetRoleType())))

	if err := validateMessage(ncv.Share.Share, msg); err != nil {
		logger.Debug("❌ got invalid message", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case genesisspectypes.SSVConsensusMsgType:
		signedMsg := &specqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			logger.Debug("❗ failed to get consensus Message from network Message", zap.Error(err))
			return
		}
		// only supports decided msg's
		if signedMsg.Message.MsgType != specqbft.CommitMsgType || !ncv.Share.HasQuorum(len(signedMsg.Signers)) {
			return
		}

		logger = logger.With(fields.GenesisHeight(signedMsg.Message.Height))

		if decided, err := ncv.qbftController.ProcessMsg(logger, signedMsg); err != nil {
			logger.Debug("❌ failed to process message",
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
					logger.Debug("❗failed to save instance", zap.Error(err))
				} else {
					logger.Debug("💾 saved instance")
				}
			}
		}
		return
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
