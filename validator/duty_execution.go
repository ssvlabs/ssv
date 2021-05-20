package validator

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	valcheck2 "github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/node/valcheck"
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
func (v *Validator) waitForSignatureCollection(logger *zap.Logger, identifier []byte, sigRoot []byte, signaturesCount int, committiee map[uint64]*proto.Node) (map[uint64][]byte, error) {
	// Collect signatures from other nodes
	// TODO - change signature count to min threshold
	signatures := make(map[uint64][]byte, signaturesCount)
	signedIndxes := make([]uint64, 0)
	lock := sync.Mutex{}
	done := false
	var err error

	// start timeout
	go func() {
		<-time.After(v.SignatureCollectionTimeout)
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
		if msg := v.msgQueue.PopMessage(msgqueue.SigRoundIndexKey(identifier)); msg != nil {
			if len(msg.SignedMessage.SignerIds) == 0 { // no signer, empty sig
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
			logger.Info("collected valid signature", zap.Uint64("node_id", msg.SignedMessage.SignerIds[0]))

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
func (v *Validator) postConsensusDutyExecution(ctx context.Context, logger *zap.Logger, identifier []byte, decidedValue []byte, signaturesCount int, role beacon.Role, duty *ethpb.DutiesResponse_Duty) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := v.signDuty(ctx, decidedValue, role, duty, v.ValidatorShare.ShareKey)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}

	// TODO - should we construct it better?
	if err := v.network.BroadcastSignature(&proto.SignedMessage{
		Message: &proto.Message{
			Lambda:      identifier,
			ValidatorPk: duty.PublicKey,
		},
		Signature: sig,
		SignerIds: []uint64{v.ValidatorShare.NodeID},
	}); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasting partial signature post consensus")

	signatures, err := v.waitForSignatureCollection(logger, identifier, root, signaturesCount, v.ValidatorShare.Committee)

	// clean queue for messages, we don't need them anymore.
	v.msgQueue.PurgeIndexedMessages(msgqueue.SigRoundIndexKey(identifier))

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

func (v *Validator) comeToConsensusOnInputValue(ctx context.Context, logger *zap.Logger, slot uint64, role beacon.Role, duty *ethpb.DutiesResponse_Duty) (int, []byte, []byte, error) {
	var inputByts []byte
	var err error
	var valCheckInstance valcheck2.ValueCheck
	switch role {
	case beacon.RoleAttester:
		attData, err := v.beacon.GetAttestationData(ctx, slot, duty.GetCommitteeIndex())
		if err != nil {
			return 0, nil, nil, errors.Wrap(err, "failed to get attestation data")
		}

		d := &proto.InputValue_Attestation{
			Attestation: &ethpb.Attestation{
				Data: attData,
			},
		}
		inputByts, err = json.Marshal(d)
		if err != nil {
			return 0, nil, nil, errors.Errorf("failed on attestation role: %s", role.String())
		}
		valCheckInstance = &valcheck.AttestationValueCheck{}
	//case beacon.RoleAggregator:
	//	aggData, err := v.beacon.GetAggregationData(ctx, slot, duty.GetCommitteeIndex())
	//	if err != nil {
	//		return 0, nil, errors.Wrap(err, "failed to get aggregation data")
	//	}
	//
	//	inputValue.Data = &proto.InputValue_AggregationData{
	//		AggregationData: aggData,
	//	}
	//case beacon.RoleProposer:
	//	block, err := v.beacon.GetProposalData(ctx, slot)
	//	if err != nil {
	//		return 0, nil, errors.Wrap(err, "failed to get proposal block")
	//	}
	//
	//	inputValue.Data = &proto.InputValue_BeaconBlock{
	//		BeaconBlock: block,
	//	}
	default:
		return 0, nil, nil, errors.Errorf("unknown role: %s", role.String())
	}

	l := logger.With(zap.String("role", role.String()))

	if err != nil {
		return 0, nil, nil, errors.Wrap(err, "failed to marshal input value")
	}

	identifier := []byte(fmt.Sprintf("%d_%s", slot, role.String()))

	if _, ok := v.ibfts[role]; !ok {
		v.logger.Error("no ibft for this role", zap.Any("role", role))
	}

	// calculate next seq
	seqNumber, err := v.ibfts[role].NextSeqNumber()
	if err != nil {
		return 0, nil, nil, errors.Wrap(err, "failed to calculate next sequence number")
	}

	decided, signaturesCount, decidedValue := v.ibfts[role].StartInstance(ibft.StartOptions{
		Duty:           duty,
		ValidatorShare: *v.ValidatorShare,
		Logger:         l,
		ValueCheck:     valCheckInstance,
		Identifier:     identifier,
		SeqNumber:      seqNumber,
		Value:          inputByts,
	})

	if !decided {
		return 0, nil, nil, errors.New("ibft did not decide, not executing role")
	}
	return signaturesCount, decidedValue, identifier, nil
}

// ExecuteDuty by slotQueue
func (v *Validator) ExecuteDuty(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty) {
	logger := v.logger.With(zap.Time("start_time", v.getSlotStartTime(slot)),
		zap.Uint64("committee_index", duty.GetCommitteeIndex()),
		zap.Uint64("slot", slot))

	roles, err := v.beacon.RolesAt(ctx, slot, duty, v.ValidatorShare.ValidatorPK, v.ValidatorShare.ShareKey)
	if err != nil {
		logger.Error("failed to get roles for duty", zap.Error(err))
		return
	}

	for _, role := range roles {
		go func(role beacon.Role) {
			l := logger.With(zap.String("role", role.String()))

			signaturesCount, decidedValue, identifier, err := v.comeToConsensusOnInputValue(ctx, logger, slot, role, duty)
			if err != nil {
				logger.Error("could not come to consensus", zap.Error(err))
				return
			}

			// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
			logger.Info("GOT CONSENSUS", zap.Any("inputValueHex", hex.EncodeToString(decidedValue)))

			// Sign, aggregate and broadcast signature
			if err := v.postConsensusDutyExecution(
				ctx,
				l,
				identifier,
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
