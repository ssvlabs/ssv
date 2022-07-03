package controller

import (
	"encoding/hex"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
)

// ProcessSignatureMessage aggregate signature messages and broadcasting when quorum achieved
func (c *Controller) ProcessSignatureMessage(msg *message.SignedPostConsensusMessage) error {
	if c.SignatureState.getState() != StateRunning {
		c.Logger.Warn("try to process signature message but timer state is not running. can't process message.", zap.String("state", c.SignatureState.getState().toString()))
		return nil
	}

	//	validate message
	if len(msg.GetSigners()) == 0 {
		c.Logger.Warn("missing signers", zap.Any("msg", msg))
		return nil
	}
	//if len(msg.GetSignature()) == 0 { // no KeyManager, empty sig
	if len(msg.Message.DutySignature) == 0 { // TODO need to add sig to msg and not use this sig
		c.Logger.Warn("missing duty signature", zap.Any("msg", msg))
		return nil
	}
	logger := c.Logger.With(zap.Uint64("signer_id", uint64(msg.GetSigners()[0])))

	//	check if already exist, if so, ignore
	if _, found := c.SignatureState.signatures[msg.GetSigners()[0]]; found { // sig already exists
		c.Logger.Debug("sig already known, skip")
		return nil
	}

	logger.Info("collected valid signature", zap.String("sig", hex.EncodeToString(msg.Message.DutySignature)), zap.Any("msg", msg))

	if err := c.verifyPartialSignature(msg.Message.DutySignature, c.SignatureState.root, msg.GetSigners()[0], c.ValidatorShare.Committee); err != nil { // TODO need to add sig to msg and not use this sig
		c.Logger.Warn("received invalid signature", zap.Error(err))
		return nil
	}

	logger.Info("signature verified")

	c.SignatureState.signatures[msg.GetSigners()[0]] = msg.Message.DutySignature
	if len(c.SignatureState.signatures) >= c.SignatureState.sigCount {
		c.Logger.Info("collected enough signature to reconstruct...", zap.Int("signatures", len(c.SignatureState.signatures)))
		c.SignatureState.stopTimer()

		// clean queue consensus & default messages that is <= c.signatureState.height, we don't need them anymore
		height := c.SignatureState.getHeight()
		c.Q.Clean(
			msgqueue.SignedPostConsensusMsgCleaner(c.Identifier, height),
		)

		err := c.broadcastSignature()
		c.SignatureState.clear()
		return err
	}
	return nil
}

// broadcastSignature reconstruct sigs and broadcast to network
func (c *Controller) broadcastSignature() error {
	// Reconstruct signatures
	if err := c.reconstructAndBroadcastSignature(c.SignatureState.signatures, c.SignatureState.root, c.SignatureState.valueStruct, c.SignatureState.duty); err != nil {
		return errors.Wrap(err, "failed to reconstruct and broadcast signature")
	}
	c.Logger.Info("Successfully submitted role!")
	return nil
}

// PostConsensusDutyExecution signs the eth2 duty after iBFT came to consensus and start signature state
func (c *Controller) PostConsensusDutyExecution(logger *zap.Logger, height message.Height, decidedValue []byte, signaturesCount int, duty *beaconprotocol.Duty) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := c.signDuty(decidedValue, duty)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}
	ssvMsg, err := c.generateSignatureMessage(sig, root, height)
	if err != nil {
		return errors.Wrap(err, "failed to generate sig message")
	}
	if err := c.Network.Broadcast(ssvMsg); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasting partial signature post consensus")

	//	start timer, clear new map and set var's
	c.SignatureState.start(c.Logger, height, signaturesCount, root, valueStruct, duty)
	return nil
}

// generateSignatureMessage returns postConsensus type ssv message with signature signed message
func (c *Controller) generateSignatureMessage(sig []byte, root []byte, height message.Height) (message.SSVMessage, error) {
	SignedMsg := &message.SignedPostConsensusMessage{
		Message: &message.PostConsensusMessage{
			Height:          height,
			DutySignature:   sig,
			DutySigningRoot: root,
			Signers:         []message.OperatorID{c.ValidatorShare.NodeID},
		},
		Signature: sig, // TODO should be msg sig and not decided sig
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
