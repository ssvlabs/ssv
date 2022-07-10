package controller

import (
	"encoding/hex"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
)

// ProcessPostConsensusMessage aggregates partial signature messages and broadcasting when quorum achieved
func (c *Controller) ProcessPostConsensusMessage(msg *ssv.SignedPartialSignatureMessage) error {
	if c.signatureState.getState() != StateRunning {
		c.logger.Warn(
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
		c.logger.Warn("could not validate partial signature message", zap.Any("msg", msg))
		return nil
	}
	logger := c.logger.With(zap.Uint64("signer_id", uint64(msg.GetSigners()[0])))
	logger.Info("all the msg signatures were verified",
		zap.String("msg signature", hex.EncodeToString(msg.GetSignature())),
		zap.String("msg beacon signature", hex.EncodeToString(msg.Messages[0].PartialSignature)),
		zap.Any("msg", msg),
	)

	// TODO: do we need this check? [<oleg>]
	//	check if already exist, if so, ignore
	if _, found := c.signatureState.signatures[message.OperatorID(msg.GetSigners()[0])]; found {
		c.logger.Debug("sig already known, skip")
		return nil
	}

	c.signatureState.signatures[message.OperatorID(msg.GetSigners()[0])] = msg.Messages[0].PartialSignature
	if len(c.signatureState.signatures) >= c.signatureState.sigCount {
		c.logger.Info("collected enough signature to reconstruct",
			zap.Int("signatures", len(c.signatureState.signatures)),
		)
		c.signatureState.stopTimer()

		// clean queue consensus & default messages that is <= c.signatureState.height, we don't need them anymore
		height := c.signatureState.getHeight()
		c.q.Clean(
			msgqueue.SignedPostConsensusMsgCleaner(c.Identifier, height),
		)

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
	ssvMsg, err := c.generateSignatureMessage(sig, root, height)
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
