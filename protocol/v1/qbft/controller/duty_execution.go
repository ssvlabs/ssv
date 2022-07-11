package controller

import (
	"encoding/hex"

	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
)

// ProcessPostConsensusMessage aggregates partial signature messages and broadcasting when quorum achieved
func (c *Controller) ProcessPostConsensusMessage(msg *ssv.SignedPartialSignatureMessage) error {
	if c.signatureState.getState() != StateRunning {
		c.Logger.Warn(
			"trying to process post consensus signature message but timer state is not running. can't process message.",
			zap.String("state", c.signatureState.getState().toString()),
		)
		return nil
	}

	// adjust share committee to the spec
	committee := make([]*types.Operator, 0)
	for _, node := range c.ValidatorShare.Committee {
		committee = append(committee, &types.Operator{
			OperatorID: types.OperatorID(node.IbftID),
			PubKey:     node.Pk,
		})
	}

	if err := message.ValidatePartialSigMsg(msg, committee, c.signatureState.duty.Slot); err != nil {
		c.Logger.Warn("could not validate partial signature message", zap.Any("msg", msg))
		return nil
	}
	logger := c.Logger.With(zap.Uint64("signer_id", uint64(msg.GetSigners()[0])))
	logger.Info("all the msg signatures were verified",
		zap.String("msg signature", hex.EncodeToString(msg.GetSignature())),
		zap.String("msg beacon signature", hex.EncodeToString(msg.Messages[0].PartialSignature)),
		zap.Any("msg", msg),
	)

	// TODO: do we need this check? [<oleg>]
	//	check if already exist, if so, ignore
	if _, found := c.SignatureState.signatures[message.OperatorID(msg.GetSigners()[0])]; found {
		c.Logger.Debug("sig already known, skip")
		return nil
	}

	c.SignatureState.signatures[message.OperatorID(msg.GetSigners()[0])] = msg.Messages[0].PartialSignature
	if len(c.SignatureState.signatures) >= c.SignatureState.sigCount {
		c.Logger.Info("collected enough signature to reconstruct",
			zap.Int("signatures", len(c.SignatureState.signatures)),
		)
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
