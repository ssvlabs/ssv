package controller

import (
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ProcessSignatureMessage aggregate signature messages and broadcasting when quorum achieved
func (c *Controller) ProcessSignatureMessage(msg *message.SignedPostConsensusMessage) error {
	if c.signatureState.getState() != StateRunning { // TODO might need ot check only if timeout ? (:Niv)
		return errors.Errorf("signature timer state is not running. state - %s", c.signatureState.getState().toString())
	}

	//	validate message
	if len(msg.GetSigners()) == 0 { // no KeyManager, empty sig
		c.logger.Error("missing KeyManager id", zap.Any("msg", msg))
		return nil
	}
	if len(msg.GetSignature()) == 0 { // no KeyManager, empty sig
		c.logger.Error("missing sig", zap.Any("msg", msg))
		return nil
	}

	//	check if already exist, if so, ignore
	if _, found := c.signatureState.signatures[msg.GetSigners()[0]]; found { // sig already exists
		c.logger.Debug("sig already known, skip")
		return nil
	}

	c.logger.Info("collected valid signature", zap.Uint64("node_id", uint64(msg.GetSigners()[0])), zap.Any("msg", msg))

	// 	verifyPartialSignature
	if err := c.verifyPartialSignature(msg.GetSignature(), c.signatureState.root, msg.GetSigners()[0], c.ValidatorShare.Committee); err != nil {
		c.logger.Error("received invalid signature", zap.Error(err))
		return nil
	}

	c.logger.Info("signature verified", zap.Uint64("node_id", uint64(msg.GetSigners()[0])))

	c.signatureState.signatures[msg.GetSigners()[0]] = msg.GetSignature()
	if len(c.signatureState.signatures) >= c.signatureState.sigCount {
		c.logger.Info("collected enough signature to reconstruct...", zap.Int("signatures", len(c.signatureState.signatures)))
		c.signatureState.stopTimer()

		// clean queue for messages, we don't need them anymore.
		c.q.Purge(msgqueue.SignedMsgIndex(message.SSVDecidedMsgType, c.Identifier, c.signatureState.height, message.CommitMsgType))

		err := c.broadcastSignature()
		c.signatureState.clear()
		return err
	}
	return nil
}

// broadcastSignature reconstruct sigs and broadcast to network
func (c *Controller) broadcastSignature() error {
	// Reconstruct signatures
	if err := c.reconstructAndBroadcastSignature(c.signatureState.signatures, c.signatureState.root, c.signatureState.valueStruct, c.signatureState.duty); err != nil {
		return errors.Wrap(err, "failed to reconstruct and broadcast signature")
	}
	c.logger.Info("Successfully submitted role!")
	return nil
}

// PostConsensusDutyExecution signs the eth2 duty after iBFT came to consensus and start signature state
func (c *Controller) PostConsensusDutyExecution(logger *zap.Logger, height message.Height, decidedValue []byte, signaturesCount int, duty *beaconprotocol.Duty) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := c.signDuty(decidedValue, duty)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}
	ssvMsg, err := c.generateSignatureMessage(sig, height)
	if err != nil {
		return errors.Wrap(err, "failed to generate sig message")
	}
	if err := c.network.Broadcast(ssvMsg); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasting partial signature post consensus")

	//	start timer, clear new map and set var's
	c.signatureState.start(c.logger, height, signaturesCount, root, valueStruct, duty)
	return nil
}

// generateSignatureMessage returns postConsensus type ssv message with signature signed message
func (c *Controller) generateSignatureMessage(sig []byte, height message.Height) (message.SSVMessage, error) {
	// TODO - should we construct it better?
	SignedMsg := &message.SignedPostConsensusMessage{
		Message: &message.PostConsensusMessage{
			Height:          height,
			DutySignature:   sig,
			DutySigningRoot: nil,
			Signers:         nil,
		},
		Signature: nil,
		Signers:   []message.OperatorID{c.ValidatorShare.NodeID},
	}

	encodedSignedMsg, err := SignedMsg.Encode()
	if err != nil {
		return message.SSVMessage{}, err
	}
	return message.SSVMessage{
		MsgType: message.SSVPostConsensusMsgType,
		ID:      c.GetIdentifier(), // TODO this is the right id? (:Niv)
		Data:    encodedSignedMsg,
	}, nil
}
