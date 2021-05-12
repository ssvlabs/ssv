package ibft

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/ibft/sync"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ProcessDecidedMessage is responsible for processing an incoming decided message.
// If the decided message is known or belong to the current executing instance, do nothing.
// Else perform a sync operation
/* From https://arxiv.org/pdf/2002.03613.pdf
We can omit this if we assume some mechanism external to the consensus algorithm that ensures
synchronization of decided values.
upon receiving a valid hROUND-CHANGE, λi, −, −, −i message from pj ∧ pi has decided
by calling Decide(λi,− , Qcommit) do
	send Qcommit to process pj
*/
func (i *ibftImpl) ProcessDecidedMessage(msg *proto.SignedMessage) {
	if err := i.params.VerifySignedMessage(msg); err != nil {
		i.logger.Error("could not verify decided msg", zap.Error(err))
		return
	}

	i.logger.Info("received highest decided", zap.Uint64("seq number", msg.Message.SeqNumber), zap.Uint64s("node ids", msg.SignerIds))

	// if we already have this in storage, pass
	res, err := i.decidedMsgKnown(msg)
	if err != nil {
		i.logger.Error("can't check if decided msg is known", zap.Error(err))
		return
	}
	if res {
		return
	}

	shouldSync, err := i.decidedRequiresSync(msg)
	if err != nil {
		i.logger.Error("can't check decided msg", zap.Error(err))
		return
	}
	if shouldSync {
		if i.currentInstance != nil {
			i.currentInstance.Stop()
		}
		// sync
		s := sync.NewHistorySync(msg.Message.ValidatorPk, i.network, i.params)
		go s.Start()
	}
}

func (i *ibftImpl) decidedMsgKnown(msg *proto.SignedMessage) (bool, error) {
	found, err := i.ibftStorage.GetDecided(msg.Message.ValidatorPk, msg.Message.SeqNumber)
	if err != nil {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	return found != nil, nil
}

// decidedForCurrentInstance returns true if msg has same seq number is current instance
func (i *ibftImpl) decidedForCurrentInstance(msg *proto.SignedMessage) bool {
	return i.currentInstance != nil && i.currentInstance.State.SeqNumber == msg.Message.SeqNumber
}

// decidedRequiresSync returns true if:
// 		- highest known seq lower than msg seq
// 		- AND msg is not for current instance
func (i *ibftImpl) decidedRequiresSync(msg *proto.SignedMessage) (bool, error) {
	if i.decidedForCurrentInstance(msg) {
		return false, nil
	}

	if msg.Message.SeqNumber == 0 {
		return false, nil
	}

	highest, err := i.ibftStorage.GetHighestDecidedInstance(msg.Message.ValidatorPk)
	if err != nil {
		return false, errors.Wrap(err, "could not get highest decided instance from storage")
	}
	return highest.Message.SeqNumber < msg.Message.SeqNumber, nil
}
