package validator

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ibft/genesisstorage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/genesis/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/genesis/qbft/controller"
	genesisqueue "github.com/ssvlabs/ssv/protocol/genesis/ssv/queue"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

type NonCommitteeValidator struct {
	Share          *types.SSVShare
	Storage        *genesisstorage.QBFTStores
	qbftController *qbftcontroller.Controller
}

func NewNonCommitteeValidator(logger *zap.Logger, identifier genesisspectypes.MessageID, opts Options) *NonCommitteeValidator {
	// currently, only need domain & genesisstorage
	config := &qbft.Config{
		Domain:                genesistypes.GetDefaultDomain(),
		Storage:               opts.Storage.Get(identifier.GetRoleType()),
		Network:               opts.Network,
		SignatureVerification: true,
	}
	ctrl := qbftcontroller.NewController(identifier[:], opts.Operator, config, opts.FullNode)
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

func (ncv *NonCommitteeValidator) ProcessMessage(logger *zap.Logger, msg *genesisqueue.GenesisSSVMessage) {
	logger = logger.With(fields.PubKey(msg.MsgID.GetPubKey()), fields.GenesisRole(msg.MsgID.GetRoleType()))

	if err := validateMessage(ncv.Share.Share, msg); err != nil {
		logger.Debug("❌ got invalid message", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case genesisspectypes.SSVConsensusMsgType:
		signedMsg := &genesisspecqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			logger.Debug("❗ failed to get consensus Message from network Message", zap.Error(err))
			return
		}
		// only supports decided msg's
		if signedMsg.Message.MsgType != genesisspecqbft.CommitMsgType || !ncv.Share.HasQuorum(len(signedMsg.Signers)) {
			return
		}

		logger = logger.With(fields.Height(specqbft.Height(signedMsg.Message.Height)))

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
