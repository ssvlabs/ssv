package validator

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type NonCommitteeValidator struct {
	Share          *types.SSVShare
	Storage        *storage.QBFTStores
	qbftController *qbftcontroller.Controller
}

func NewNonCommitteeValidator(logger *zap.Logger, identifier spectypes.MessageID, opts Options) *NonCommitteeValidator {
	// TODO: fork support
	// probably need to create custom MessageID type that contains our custom RunnerRole type to support both forks
	specRole := types.RunnerRoleFromSpec(identifier.GetRoleType())

	config := &qbft.Config{
		Domain:                types.GetDefaultDomain(),
		Storage:               opts.Storage.Get(specRole),
		Network:               opts.Network,
		SignatureVerification: true,
	}

	ctrl := qbftcontroller.NewController(identifier[:], &opts.SSVShare.Operator, config, opts.FullNode)
	ctrl.StoredInstances = make(qbftcontroller.InstanceContainer, 0, nonCommitteeInstanceContainerCapacity(opts.FullNode))
	ctrl.NewDecidedHandler = opts.NewDecidedHandler
	if _, err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		logger.Debug("❗ failed to load highest instance", zap.Error(err))
	}

	return &NonCommitteeValidator{
		Share:          opts.SSVShare,
		Storage:        opts.Storage,
		qbftController: ctrl,
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(logger *zap.Logger, msg *queue.DecodedSSVMessage) {
	logger = logger.With(fields.PubKey(msg.MsgID.GetSenderID()), fields.SpecRunnerRole(msg.MsgID.GetRoleType()))

	if err := validateMessage(ncv.Share.Share, msg); err != nil {
		logger.Debug("❌ got invalid message", zap.Error(err))
		return
	}

	signedMessage := msg.SignedSSVMessage

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &specqbft.Message{}
		if err := qbftMsg.Decode(msg.GetData()); err != nil {
			logger.Debug("❗ failed to get consensus Message from network Message", zap.Error(err))
			return
		}
		// only supports decided msg's
		if qbftMsg.MsgType != specqbft.CommitMsgType || !ncv.Share.HasQuorum(len(signedMessage.GetOperatorIDs())) {
			return
		}

		logger = logger.With(fields.Height(qbftMsg.Height))

		if decided, err := ncv.qbftController.ProcessMsg(logger, signedMessage); err != nil {
			logger.Debug("❌ failed to process message",
				zap.Uint64("msg_height", uint64(qbftMsg.Height)),
				zap.Any("signers", signedMessage.GetOperatorIDs()),
				zap.Error(err))
		} else if decided != nil {
			if inst := ncv.qbftController.StoredInstances.FindInstance(qbftMsg.Height); inst != nil {
				logger := logger.With(
					zap.Uint64("msg_height", uint64(qbftMsg.Height)),
					zap.Uint64("ctrl_height", uint64(ncv.qbftController.Height)),
					zap.Any("signers", signedMessage.GetOperatorIDs()),
				)
				if err = ncv.qbftController.SaveInstance(inst, signedMessage); err != nil {
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
