package validator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	ibftvalcheck "github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/valcheck"
	"github.com/pkg/errors"
	"sync"
	"time"

	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"
)

// waitForSignatureCollection waits for inbound signatures, collects them or times out if not.
func (v *Validator) waitForSignatureCollection(logger *zap.Logger, identifier []byte, seqNumber uint64, sigRoot []byte, signaturesCount int, committiee map[uint64]*proto.Node) (map[uint64][]byte, error) {
	// Collect signatures from other nodes
	// TODO - change signature count to min threshold
	signatures := make(map[uint64][]byte, signaturesCount)
	signedIndxes := make([]uint64, 0)
	lock := sync.Mutex{}
	done := false
	var err error

	// start timeout
	go func() {
		defer func() {
			if r := recover(); r != nil {
				v.logger.Error("recovered in waitForSignatureCollection", zap.Any("error", r))
			}
		}()
		<-time.After(v.signatureCollectionTimeout)
		lock.Lock()
		defer lock.Unlock()
		err = errors.Errorf("timed out waiting for post consensus signatures, received %d", len(signedIndxes))
		done = true
	}()
	// loop through messages until timeout
	for {
		lock.Lock()
		if done {
			lock.Unlock()
			break
		} else {
			lock.Unlock()
		}
		if msg := v.msgQueue.PopMessage(msgqueue.SigRoundIndexKey(identifier, seqNumber)); msg != nil {
			if len(msg.SignedMessage.SignerIds) == 0 { // no signer, empty sig
				v.logger.Error("missing signer id", zap.Any("msg", msg.SignedMessage))
				continue
			}
			if len(msg.SignedMessage.Signature) == 0 { // no signer, empty sig
				v.logger.Error("missing sig", zap.Any("msg", msg.SignedMessage))
				continue
			}
			if _, found := signatures[msg.SignedMessage.SignerIds[0]]; found { // sig already exists
				continue
			}

			logger.Info("collected valid signature", zap.Uint64("node_id", msg.SignedMessage.SignerIds[0]), zap.Any("msg", msg))

			// verify sig
			if err := v.verifyPartialSignature(msg.SignedMessage.Signature, sigRoot, msg.SignedMessage.SignerIds[0], committiee); err != nil {
				logger.Error("received invalid signature", zap.Error(err))
				continue
			}
			logger.Info("signature verified", zap.Uint64("node_id", msg.SignedMessage.SignerIds[0]))

			lock.Lock()
			signatures[msg.SignedMessage.SignerIds[0]] = msg.SignedMessage.Signature
			signedIndxes = append(signedIndxes, msg.SignedMessage.SignerIds[0])
			if len(signedIndxes) >= signaturesCount {
				done = true
				break
			}
			lock.Unlock()
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}
	return signatures, err
}

// postConsensusDutyExecution signs the eth2 duty after iBFT came to consensus,
// waits for others to sign, collect sigs, reconstruct and broadcast the reconstructed signature to the beacon chain
func (v *Validator) postConsensusDutyExecution(ctx context.Context, logger *zap.Logger, seqNumber uint64, decidedValue []byte, signaturesCount int, role beacon.Role, duty *ethpb.DutiesResponse_Duty) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := v.signDuty(ctx, decidedValue, role, duty, v.Share.ShareKey)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}

	identifier := v.ibfts[role].GetIdentifier()
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
	if err := v.reconstructAndBroadcastSignature(ctx, logger, signatures, root, valueStruct, role, duty); err != nil {
		return errors.Wrap(err, "failed to reconstruct and broadcast signature")
	}
	logger.Info("Successfully submitted role!")
	return nil
}

func (v *Validator) comeToConsensusOnInputValue(ctx context.Context, logger *zap.Logger, slot uint64, role beacon.Role, duty *ethpb.DutiesResponse_Duty) (int, []byte, uint64, error) {
	var inputByts []byte
	var err error
	var valCheckInstance ibftvalcheck.ValueCheck

	l := logger.With(zap.String("role", role.String()))
	if _, ok := v.ibfts[role]; !ok {
		return 0, nil, 0, errors.Errorf("no ibft for this role [%s]", role.String())
	}

	switch role {
	case beacon.RoleAttester:
		attData, err := v.beacon.GetAttestationData(ctx, slot, duty.GetCommitteeIndex())
		if err != nil {
			return 0, nil, 0, errors.Wrap(err, "failed to get attestation data")
		}

		d := &proto.InputValue_Attestation{
			Attestation: &ethpb.Attestation{
				Data: attData,
			},
		}
		inputByts, err = json.Marshal(d)
		if err != nil {
			return 0, nil, 0, errors.Errorf("failed to marshal on attestation role: %s", role.String())
		}
		valCheckInstance = &valcheck.AttestationValueCheck{}
	case beacon.RoleAggregator:
		aggData, err := v.beacon.GetAggregationData(ctx, duty, v.Share.PublicKey, v.Share.ShareKey)
		if err != nil {
			return 0, nil, 0, errors.Wrap(err, "failed to get aggregation data")
		}

		d := &proto.InputValue_AggregationData{
			AggregationData: aggData,
		}
		inputByts, err = json.Marshal(d)
		if err != nil {
			return 0, nil, 0, errors.Errorf("failed to marshal on aggregation role: %s", role.String())
		}
		valCheckInstance = &valcheck.AggregatorValueCheck{}
	case beacon.RoleProposer:
		block, err := v.beacon.GetProposalData(ctx, slot, v.Share.ShareKey)
		if err != nil {
			return 0, nil, 0, errors.Wrap(err, "failed to get proposal block")
		}

		d := &proto.InputValue_BeaconBlock{
			BeaconBlock: block,
		}
		inputByts, err = json.Marshal(d)
		if err != nil {
			return 0, nil, 0, errors.Errorf("failed to marshal on proposer role: %s", role.String())
		}
		valCheckInstance = &valcheck.ProposerValueCheck{}
	default:
		return 0, nil, 0, errors.Errorf("unknown role: %s", role.String())
	}

	// calculate next seq
	seqNumber, err := v.ibfts[role].NextSeqNumber()
	if err != nil {
		return 0, nil, 0, errors.Wrap(err, "failed to calculate next sequence number")
	}

	resChan, err := v.ibfts[role].StartInstance(ibft.StartOptions{
		Duty:           duty,
		ValidatorShare: *v.Share,
		Logger:         l,
		ValueCheck:     valCheckInstance,
		SeqNumber:      seqNumber,
		Value:          inputByts,
	})
	if err != nil {
		return 0, nil, 0, errors.WithMessage(err, "ibft instance failed")
	}

	result := <-resChan
	if result == nil {
		return 0, nil, seqNumber, errors.Wrap(err, "instance result returned nil")
	}
	if !result.Decided {
		if result.Error != nil {
			return 0, nil, seqNumber, errors.Wrap(err, "instance did not decide")
		}
		return 0, nil, seqNumber, errors.New("instance did not decide")
	}
	return len(result.Msg.SignerIds), result.Msg.Message.Value, seqNumber, nil
}

// ExecuteDuty by slotQueue
func (v *Validator) ExecuteDuty(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty) {
	logger := v.logger.With(zap.Time("start_time", v.getSlotStartTime(slot)),
		zap.Uint64("committee_index", duty.GetCommitteeIndex()),
		zap.Uint64("slot", slot))

	logger.Debug("executing duty...")
	roles, err := v.beacon.RolesAt(ctx, slot, duty, v.Share.PublicKey, v.Share.ShareKey)
	if err != nil {
		logger.Error("failed to get roles for duty", zap.Error(err))
		return
	}

	for _, role := range roles {
		go func(role beacon.Role) {
			l := logger.With(zap.String("role", role.String()))
			l.Debug("starting duty role")

			signaturesCount, decidedValue, seqNumber, err := v.comeToConsensusOnInputValue(ctx, logger, slot, role, duty)
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
				role,
				duty,
			); err != nil {
				logger.Error("could not execute duty", zap.Error(err))
				return
			}
		}(role)
	}
}
