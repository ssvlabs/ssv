package validator

import (
	"encoding/hex"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"time"

	ibftvalcheck "github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/pkg/errors"

	"go.uber.org/zap"
)

// waitForSignatureCollection waits for inbound signatures, collects them or times out if not.
func (v *Validator) waitForSignatureCollection(logger *zap.Logger, identifier []byte, height message.Height, sigRoot []byte, signaturesCount int, committiee map[message.OperatorID]*message.Node) (map[uint64][]byte, error) {
	// Collect signatures from other nodes
	// TODO - change signature count to min threshold
	signatures := make(map[uint64][]byte, signaturesCount)
	signedIndxes := make([]uint64, 0)
	var err error
	timer := time.NewTimer(v.signatureCollectionTimeout)
	// loop through messages until timeout
SigCollectionLoop:
	for {
		select {
		case <-timer.C:
			err = errors.Errorf("timed out waiting for post consensus signatures, received %d", len(signedIndxes))
			break SigCollectionLoop
		default:
			if msg := v.msgQueue.PopMessage(msgqueue.SigRoundIndexKey(identifier, height)); msg != nil {
				if len(msg.SignedMessage.SignerIds) == 0 { // no KeyManager, empty sig
					v.logger.Error("missing KeyManager id", zap.Any("msg", msg.SignedMessage))
					continue SigCollectionLoop
				}
				if len(msg.SignedMessage.Signature) == 0 { // no KeyManager, empty sig
					v.logger.Error("missing sig", zap.Any("msg", msg.SignedMessage))
					continue SigCollectionLoop
				}
				if _, found := signatures[msg.SignedMessage.SignerIds[0]]; found { // sig already exists
					continue SigCollectionLoop
				}

				logger.Info("collected valid signature", zap.Uint64("node_id", msg.SignedMessage.SignerIds[0]), zap.Any("msg", msg))

				// verify sig
				if err := v.verifyPartialSignature(msg.SignedMessage.Signature, sigRoot, msg.SignedMessage.SignerIds[0], committiee); err != nil {
					logger.Error("received invalid signature", zap.Error(err))
					continue SigCollectionLoop
				}
				logger.Info("signature verified", zap.Uint64("node_id", msg.SignedMessage.SignerIds[0]))

				signatures[msg.SignedMessage.SignerIds[0]] = msg.SignedMessage.Signature
				signedIndxes = append(signedIndxes, msg.SignedMessage.SignerIds[0])
				if len(signedIndxes) >= signaturesCount {
					timer.Stop()
					break SigCollectionLoop
				}
			} else {
				time.Sleep(time.Millisecond * 100)
			}
		}
	}

	return signatures, err
}

// postConsensusDutyExecution signs the eth2 duty after iBFT came to consensus,
// waits for others to sign, collect sigs, reconstruct and broadcast the reconstructed signature to the beacon chain
func (v *Validator) postConsensusDutyExecution(logger *zap.Logger, height message.Height, decidedValue []byte, signaturesCount int, duty *beaconprotocol.Duty, ) error {
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

	signatures, err := v.waitForSignatureCollection(logger, identifier, height, root, signaturesCount, v.share.Committee)

	// clean queue for messages, we don't need them anymore. TODo need to check how to dump sig message! (:@niv)
	//v.msgQueue.PurgeIndexedMessages(msgqueue.SigRoundIndexKey(identifier, height))

	if err != nil {
		return err
	}
	logger.Info("collected enough signature to reconstruct...", zap.Int("signatures", len(signatures)))

	// Reconstruct signatures
	if err := v.reconstructAndBroadcastSignature(logger, signatures, root, valueStruct, duty); err != nil {
		return errors.Wrap(err, "failed to reconstruct and broadcast signature")
	}
	logger.Info("Successfully submitted role!")
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
