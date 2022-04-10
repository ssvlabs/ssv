package validator

import (
	"encoding/hex"
	ibftvalcheck "github.com/bloxapp/ssv/ibft/valcheck"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/pkg/errors"

	"go.uber.org/zap"
)

func (v *Validator) processSignatureMessage(msg *message.SignedPostConsensusMessage) error {
	// TODO do we need to check if signatureState.start() already been called?

	//if !v.signatureState.timer.Stop() { TODO handle timeout
	//	<-v.signatureState.timer.C
	//}
	//	err = errors.Errorf("timed out waiting for post consensus signatures, received %d", len(signedIndxes))

	//	validate message
	if len(msg.GetSigners()) == 0 { // no KeyManager, empty sig
		v.logger.Error("missing KeyManager id", zap.Any("msg", msg))
		return nil
	}
	if len(msg.GetSignature()) == 0 { // no KeyManager, empty sig
		v.logger.Error("missing sig", zap.Any("msg", msg))
		return nil
	}

	//	check if already exist, if so, ignore
	if _, found := v.signatureState.signatures[msg.GetSigners()[0]]; found { // sig already exists
		v.logger.Debug("sig already known, skip")
		return nil
	}

	v.logger.Info("collected valid signature", zap.Uint64("node_id", uint64(msg.GetSigners()[0])), zap.Any("msg", msg))

	// 	verifyPartialSignature
	if err := v.verifyPartialSignature(msg.GetSignature(), v.signatureState.root, msg.GetSigners()[0], v.share.Committee); err != nil {
		v.logger.Error("received invalid signature", zap.Error(err))
		return nil
	}

	v.logger.Info("signature verified", zap.Uint64("node_id", uint64(msg.GetSigners()[0])))

	v.signatureState.signatures[msg.GetSigners()[0]] = msg.GetSignature()
	if len(v.signatureState.signatures) >= v.signatureState.sigCount {
		v.logger.Info("collected enough signature to reconstruct...", zap.Int("signatures", len(v.signatureState.signatures)))
		//	 stop only timer!

		// clean queue for messages, we don't need them anymore. TODo need to check how to dump sig message! (:@niv)
		//v.msgQueue.PurgeIndexedMessages(msgqueue.SigRoundIndexKey(identifier, height))

		return v.broadcastSignature()
	}
	return nil
}

func (v *Validator) broadcastSignature() error {
	// Reconstruct signatures
	if err := v.reconstructAndBroadcastSignature(v.signatureState.signatures, v.signatureState.root, v.signatureState.valueStruct, v.signatureState.duty); err != nil {
		return errors.Wrap(err, "failed to reconstruct and broadcast signature")
	}
	v.logger.Info("Successfully submitted role!")
	return nil
}

// postConsensusDutyExecution signs the eth2 duty after iBFT came to consensus,
// waits for others to sign, collect sigs, reconstruct and broadcast the reconstructed signature to the beacon chain
func (v *Validator) postConsensusDutyExecution(logger *zap.Logger, height message.Height, decidedValue []byte, signaturesCount int, duty *beaconprotocol.Duty) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := v.signDuty(decidedValue, duty)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}

	identifier := v.ibfts[duty.Type].GetIdentifier()
	// TODO - should we construct it better?
	if err := v.network.BroadcastSignature(v.share.PublicKey.Serialize(), &message.SignedMessage{
		Message: &message.ConsensusMessage{
			MsgType:    0,
			Height:     height,
			Round:      0,
			Identifier: identifier,
			Data:       nil,
		},
		Signature: sig,
		Signers:   []message.OperatorID{v.share.NodeID},
	}); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasting partial signature post consensus")

	//	start timer, clear new map and set var's
	v.signatureState.start(signaturesCount, root, valueStruct, duty)
	return nil
}

func (v *Validator) comeToConsensusOnInputValue(logger *zap.Logger, duty *beaconprotocol.Duty) (int, []byte, message.Height, error) {
	var inputByts []byte
	var err error
	var valCheckInstance ibftvalcheck.ValueCheck

	if _, ok := v.ibfts[duty.Type]; !ok {
		return 0, nil, 0, errors.Errorf("no ibft for this role [%s]", duty.Type.String())
	}

	switch duty.Type {
	case beaconprotocol.RoleTypeAttester:
		attData, err := v.beacon.GetAttestationData(duty.Slot, duty.CommitteeIndex)
		if err != nil {
			return 0, nil, 0, errors.Wrap(err, "failed to get attestation data")
		}

		inputByts, err = attData.MarshalSSZ()
		if err != nil {
			return 0, nil, 0, errors.Errorf("failed to marshal on attestation role: %s", duty.Type.String())
		}
	default:
		return 0, nil, 0, errors.Errorf("unknown role: %s", duty.Type.String())
	}

	// calculate next seq
	seqNumber, err := v.ibfts[duty.Type].NextSeqNumber()
	if err != nil {
		return 0, nil, 0, errors.Wrap(err, "failed to calculate next sequence number")
	}

	result, err := v.ibfts[duty.Type].StartInstance(instance.ControllerStartInstanceOptions{
		Logger:          logger,
		ValueCheck:      valCheckInstance,
		SeqNumber:       seqNumber,
		Value:           inputByts,
		RequireMinPeers: true,
	})
	if err != nil {
		return 0, nil, 0, errors.WithMessage(err, "ibft instance failed")
	}
	if result == nil {
		return 0, nil, seqNumber, errors.Wrap(err, "instance result returned nil")
	}
	if !result.Decided {
		return 0, nil, seqNumber, errors.New("instance did not decide")
	}

	return len(result.Msg.Signers), result.Msg.Message.Data, seqNumber, nil
}

// ExecuteDuty executes the given duty
func (v *Validator) ExecuteDuty(slot uint64, duty *beaconprotocol.Duty) {
	logger := v.logger.With(zap.Time("start_time", v.network.GetSlotStartTime(slot)),
		zap.Uint64("committee_index", uint64(duty.CommitteeIndex)),
		zap.Uint64("slot", slot),
		zap.String("duty_type", duty.Type.String()))

	metricsCurrentSlot.WithLabelValues(v.share.PublicKey.SerializeToHexStr()).Set(float64(duty.Slot))

	logger.Debug("executing duty...")
	signaturesCount, decidedValue, seqNumber, err := v.comeToConsensusOnInputValue(logger, duty)
	if err != nil {
		logger.Error("could not come to consensus", zap.Error(err))
		return
	}

	// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
	logger.Info("GOT CONSENSUS", zap.Any("inputValueHex", hex.EncodeToString(decidedValue)))

	// Sign, aggregate and broadcast signature
	if err := v.postConsensusDutyExecution(logger, seqNumber, decidedValue, signaturesCount, duty); err != nil {
		logger.Error("could not execute duty", zap.Error(err))
		return
	}
}
