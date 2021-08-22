package validator

import (
	"context"
	"encoding/hex"
	ibftvalcheck "github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"time"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"go.uber.org/zap"
)

var (
	metricsRunningIBFTsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:validator:running_ibfts_count_all",
		Help: "Count all running IBFTs",
	})
	metricsRunningIBFTs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:running_ibfts_count",
		Help: "Count all running IBFTs",
	}, []string{"pubKey"})
)

func init() {
	prometheus.Register(metricsRunningIBFTsCount)
	prometheus.Register(metricsRunningIBFTs)
}

// waitForSignatureCollection waits for inbound signatures, collects them or times out if not.
func (v *Validator) waitForSignatureCollection(logger *zap.Logger, identifier []byte, seqNumber uint64, sigRoot []byte, signaturesCount int, committiee map[uint64]*proto.Node) (map[uint64][]byte, error) {
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
			if msg := v.msgQueue.PopMessage(msgqueue.SigRoundIndexKey(identifier, seqNumber)); msg != nil {
				if len(msg.SignedMessage.SignerIds) == 0 { // no signer, empty sig
					v.logger.Error("missing signer id", zap.Any("msg", msg.SignedMessage))
					continue SigCollectionLoop
				}
				if len(msg.SignedMessage.Signature) == 0 { // no signer, empty sig
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
func (v *Validator) postConsensusDutyExecution(ctx context.Context, logger *zap.Logger, seqNumber uint64, decidedValue []byte, signaturesCount int, duty *beacon.Duty) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := v.signDuty(decidedValue, duty, v.Share.ShareKey)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}

	identifier := v.ibfts[duty.Type].GetIdentifier()
	// TODO - should we construct it better?
	if err := v.network.BroadcastSignature(v.Share.PublicKey.Serialize(), &proto.SignedMessage{
		Message: &proto.Message{
			Lambda:    identifier,
			SeqNumber: seqNumber,
		},
		Signature: sig,
		SignerIds: []uint64{v.Share.NodeID},
	}); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasting partial signature post consensus")

	signatures, err := v.waitForSignatureCollection(logger, identifier, seqNumber, root, signaturesCount, v.Share.Committee)

	// clean queue for messages, we don't need them anymore.
	v.msgQueue.PurgeIndexedMessages(msgqueue.SigRoundIndexKey(identifier, seqNumber))

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

func (v *Validator) comeToConsensusOnInputValue(logger *zap.Logger, duty *beacon.Duty) (int, []byte, uint64, error) {
	var inputByts []byte
	var err error
	var valCheckInstance ibftvalcheck.ValueCheck

	if _, ok := v.ibfts[duty.Type]; !ok {
		return 0, nil, 0, errors.Errorf("no ibft for this role [%s]", duty.Type.String())
	}

	switch duty.Type {
	case beacon.RoleTypeAttester:
		attData, err := v.beacon.GetAttestationData(duty.Slot, duty.CommitteeIndex)
		if err != nil {
			return 0, nil, 0, errors.Wrap(err, "failed to get attestation data")
		}

		inputByts, err = attData.MarshalSSZ()
		if err != nil {
			return 0, nil, 0, errors.Errorf("failed to marshal on attestation role: %s", duty.Type.String())
		}
		valCheckInstance = v.valueCheck.AttestationSlashingProtector()
	//case beacon.RoleTypeAggregator:
	//	aggData, err := v.beacon.GetAggregationData(ctx, duty, v.Share.PublicKey, v.Share.ShareKey)
	//	if err != nil {
	//		return 0, nil, 0, errors.Wrap(err, "failed to get aggregation data")
	//	}
	//
	//	d := &proto.InputValue_AggregationData{
	//		AggregationData: aggData,
	//	}
	//	inputByts, err = json.Marshal(d)
	//	if err != nil {
	//		return 0, nil, 0, errors.Errorf("failed to marshal on aggregation role: %s", role.String())
	//	}
	//	valueCheck = &valcheck.AggregatorValueCheck{}
	//case beacon.RoleTypeProposer:
	//	block, err := v.beacon.GetProposalData(ctx, slot, v.Share.ShareKey)
	//	if err != nil {
	//		return 0, nil, 0, errors.Wrap(err, "failed to get proposal block")
	//	}
	//
	//	d := &proto.InputValue_BeaconBlock{
	//		BeaconBlock: block,
	//	}
	//	inputByts, err = json.Marshal(d)
	//	if err != nil {
	//		return 0, nil, 0, errors.Errorf("failed to marshal on proposer role: %s", role.String())
	//	}
	//	valueCheck = &valcheck.ProposerValueCheck{}
	default:
		return 0, nil, 0, errors.Errorf("unknown role: %s", duty.Type.String())
	}

	// do a value check before instance starts to prevent a dead lock if all SSV instances start
	// an iBFT instance with values which are invalid which will result in them getting "stuck"
	// in infinite round changes
	if err := valCheckInstance.Check(inputByts); err != nil {
		return 0, nil, 0, errors.Wrap(err, "input value failed pre-consensus check")
	}

	// calculate next seq
	seqNumber, err := v.ibfts[duty.Type].NextSeqNumber()
	if err != nil {
		return 0, nil, 0, errors.Wrap(err, "failed to calculate next sequence number")
	}

	result, err := v.ibfts[duty.Type].StartInstance(ibft.StartOptions{
		ValidatorShare:  v.Share,
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

	return len(result.Msg.SignerIds), result.Msg.Message.Value, seqNumber, nil
}

// ExecuteDuty executes the given duty
func (v *Validator) ExecuteDuty(ctx context.Context, slot uint64, duty *beacon.Duty) {
	logger := v.logger.With(zap.Time("start_time", v.getSlotStartTime(slot)),
		zap.Uint64("committee_index", uint64(duty.CommitteeIndex)),
		zap.Uint64("slot", slot),
		zap.String("duty_type", duty.Type.String()))

	// writing metrics
	metricsRunningIBFTsCount.Inc()
	defer metricsRunningIBFTsCount.Dec()

	pubKey := v.Share.PublicKey.SerializeToHexStr()
	metricsRunningIBFTs.WithLabelValues(pubKey).Inc()
	defer metricsRunningIBFTs.WithLabelValues(pubKey).Dec()

	logger.Debug("executing duty...")
	signaturesCount, decidedValue, seqNumber, err := v.comeToConsensusOnInputValue(logger, duty)
	if err != nil {
		logger.Error("could not come to consensus", zap.Error(err))
		return
	}

	// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
	logger.Info("GOT CONSENSUS", zap.Any("inputValueHex", hex.EncodeToString(decidedValue)))

	// Sign, aggregate and broadcast signature
	if err := v.postConsensusDutyExecution(
		ctx,
		logger,
		seqNumber,
		decidedValue,
		signaturesCount,
		duty,
	); err != nil {
		logger.Error("could not execute duty", zap.Error(err))
		return
	}
}
