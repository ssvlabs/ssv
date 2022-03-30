package controller

import (
	"time"

	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// processDecidedQueueMessages is listen for all the ibft decided msg's and process them
func (i *Controller) processDecidedQueueMessages() {
	go func() {
		for {
			if decidedMsg := i.msgQueue.PopMessage(msgqueue.DecidedIndexKey(i.GetIdentifier())); decidedMsg != nil {
				i.ProcessDecidedMessage(decidedMsg.SignedMessage)
			}
			time.Sleep(time.Millisecond * 100)
		}
	}()
	i.logger.Info("decided message queue started")
}

// ValidateDecidedMsg - the main decided msg pipeline
func (i *Controller) ValidateDecidedMsg(msg *proto.SignedMessage) error {
	return i.fork.ValidateDecidedMsg(i.ValidatorShare).Run(msg)
}

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
func (i *Controller) ProcessDecidedMessage(msg *proto.SignedMessage) {
	if err := i.ValidateDecidedMsg(msg); err != nil {
		i.logger.Error("received invalid decided message", zap.Error(err), zap.Uint64s("signer ids", msg.SignerIds))
		return
	}
	logger := i.logger.With(zap.Uint64("seq number", msg.Message.SeqNumber), zap.Uint64s("signer ids", msg.SignerIds))

	logger.Debug("received valid decided msg")

	// if we already have this in storage, pass
	known, err := i.decidedMsgKnown(msg)
	if err != nil {
		logger.Error("can't check if decided msg is known", zap.Error(err))
		return
	}
	if known {
		// if decided is known, check for a more complete message (more signers)
		if ignore, _ := i.checkDecidedMessageSigners(msg); !ignore {
			if err := i.ibftStorage.SaveDecided(msg); err != nil {
				logger.Error("can't update decided message", zap.Error(err))
				return
			}
			logger.Debug("decided was updated")
			ibft.ReportDecided(i.ValidatorShare.PublicKey.SerializeToHexStr(), msg)
			return
		}
		logger.Debug("decided is known, skipped")
		return
	}

	ibft.ReportDecided(i.ValidatorShare.PublicKey.SerializeToHexStr(), msg)

	// decided for current instance
	if i.forceDecideCurrentInstance(msg) {
		return
	}

	// decided for later instances which require a full sync
	shouldSync, err := i.decidedRequiresSync(msg)
	if err != nil {
		logger.Error("can't check decided msg", zap.Error(err))
		return
	}
	if shouldSync {
		i.logger.Info("stopping current instance and syncing..")
		if err := i.SyncIBFT(); err != nil {
			logger.Error("failed sync after decided received", zap.Error(err))
		}
	}
}

// forceDecideCurrentInstance will force the current instance to decide provided a signed decided msg.
// will return true if executed, false otherwise
func (i *Controller) forceDecideCurrentInstance(msg *proto.SignedMessage) bool {
	if i.decidedForCurrentInstance(msg) {
		// stop current instance
		if i.currentInstance != nil {
			i.currentInstance.ForceDecide(msg)
		}
		return true
	}
	return false
}

// highestKnownDecided returns the highest known decided instance
func (i *Controller) highestKnownDecided() (*proto.SignedMessage, error) {
	highestKnown, _, err := i.ibftStorage.GetHighestDecidedInstance(i.GetIdentifier())
	if err != nil {
		return nil, err
	}
	return highestKnown, nil
}

func (i *Controller) decidedMsgKnown(msg *proto.SignedMessage) (bool, error) {
	_, found, err := i.ibftStorage.GetDecided(msg.Message.Lambda, msg.Message.SeqNumber)
	if err != nil {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	return found, nil
}

// checkDecidedMessageSigners checks if signers of existing decided includes all signers of the newer message
func (i *Controller) checkDecidedMessageSigners(msg *proto.SignedMessage) (bool, error) {
	decided, found, err := i.ibftStorage.GetDecided(msg.Message.Lambda, msg.Message.SeqNumber)
	if err != nil {
		return false, errors.Wrap(err, "could not get decided instance from storage")
	}
	if !found {
		return false, nil
	}
	// decided message should have at least 3 signers, so if the new decided has 4 signers -> override
	if len(decided.SignerIds) < i.ValidatorShare.CommitteeSize() && len(msg.SignerIds) > len(decided.SignerIds) {
		return false, nil
	}
	return true, nil
}

// decidedForCurrentInstance returns true if msg has same seq number is current instance
func (i *Controller) decidedForCurrentInstance(msg *proto.SignedMessage) bool {
	return i.currentInstance != nil && i.currentInstance.State().SeqNumber.Get() == msg.Message.SeqNumber
}

// decidedRequiresSync returns true if:
// 		- highest known seq lower than msg seq
// 		- AND msg is not for current instance
func (i *Controller) decidedRequiresSync(msg *proto.SignedMessage) (bool, error) {
	// if IBFT sync failed to init, trigger it again
	if !i.initSynced.Get() {
		return true, nil
	}
	if i.decidedForCurrentInstance(msg) {
		return false, nil
	}

	if msg.Message.SeqNumber == 0 {
		return false, nil
	}

	highest, found, err := i.ibftStorage.GetHighestDecidedInstance(msg.Message.Lambda)
	if !found {
		return msg.Message.SeqNumber > 0, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "could not get highest decided instance from storage")
	}
	return highest.Message.SeqNumber < msg.Message.SeqNumber, nil
}
