package validator

import (
	"context"
	"fmt"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/ibft/storage"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

type NonCommitteeValidator struct {
	ctx            context.Context
	cancel         context.CancelFunc
	logger         *zap.Logger
	Share          *types.SSVShare
	Storage        *storage.QBFTStores
	qbftController *qbftcontroller.Controller
}

func NewNonCommitteeValidator(pctx context.Context, identifier spectypes.MessageID, opts Options) *NonCommitteeValidator {
	ctx, cancel := context.WithCancel(pctx)

	// currently, only need domain & storage
	config := &types.Config{
		Domain:  types.GetDefaultDomain(),
		Storage: opts.Storage.Get(identifier.GetRoleType()),
	}
	ctrl := qbftcontroller.NewController(identifier[:], &opts.SSVShare.Share, types.GetDefaultDomain(), config)
	if err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		fmt.Printf("failed to load highest instance: %e", err)
	}

	return &NonCommitteeValidator{
		ctx:            ctx,
		cancel:         cancel,
		logger:         opts.Logger,
		Share:          opts.SSVShare,
		Storage:        opts.Storage,
		qbftController: ctrl,
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(msg *spectypes.SSVMessage) {
	logger := ncv.logger.With(zap.String("id", msg.GetID().String()))
	if err := ValidateMessage(ncv.Share.Share, msg); err != nil {
		logger.Warn("Message invalid", zap.Error(err))
		return
	}

	switch msg.GetType() {
	case spectypes.SSVConsensusMsgType:
		signedMsg := &specqbft.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			logger.Warn("could not get consensus Message from network Message", zap.Error(err))
			return
		}
		// only supports decided msg's
		if signedMsg.Message.MsgType != specqbft.CommitMsgType || !ncv.Share.HasQuorum(len(signedMsg.Signers)) {
			return
		}

		if _, err := ncv.qbftController.ProcessMsg(signedMsg); err != nil {
			logger.Warn("failed to process message", zap.Error(err))
		}
		return
	}
}

// Stop stops a Validator.
func (ncv *NonCommitteeValidator) Stop() error {
	ncv.cancel()
	return nil
}
