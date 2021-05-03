package node

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	valcheck2 "github.com/bloxapp/ssv/ibft/valcheck"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/node/valcheck"
	"github.com/bloxapp/ssv/slotqueue"
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
func (n *ssvNode) waitForSignatureCollection(logger *zap.Logger, identifier []byte, sigRoot []byte, signaturesCount int, committiee map[uint64]*proto.Node, ) (map[uint64][]byte, error) {
	// Collect signatures from other nodes
	// TODO - change signature count to min threshold
	signatures := make(map[uint64][]byte, signaturesCount)
	signedIndxes := make([]uint64, 0)
	lock := sync.Mutex{}
	done := false
	var err error

	// start timeout
	go func() {
		<-time.After(n.signatureCollectionTimeout)
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
		if msg := n.queue.PopMessage(msgqueue.SigRoundIndexKey(identifier)); msg != nil {
			if len(msg.Msg.SignerIds) == 0 { // no signer, empty sig
				continue
			}
			if _, found := signatures[msg.Msg.SignerIds[0]]; found { // sig already exists
				continue
			}

			logger.Info("collected valid signature", zap.Uint64("node_id", msg.Msg.SignerIds[0]), zap.Any("msg", msg))

			// verify sig
			if err := n.verifyPartialSignature(msg.Msg.Signature, sigRoot, msg.Msg.SignerIds[0], committiee); err != nil {
				logger.Error("received invalid signature", zap.Error(err))
				continue
			}
			logger.Info("collected valid signature", zap.Uint64("node_id", msg.Msg.SignerIds[0]))

			lock.Lock()
			signatures[msg.Msg.SignerIds[0]] = msg.Msg.Signature
			signedIndxes = append(signedIndxes, msg.Msg.SignerIds[0])
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
func (n *ssvNode) postConsensusDutyExecution(ctx context.Context, logger *zap.Logger, identifier []byte, decidedValue []byte, signaturesCount int, role beacon.Role, duty *slotqueue.Duty, ) error {
	// sign input value and broadcast
	sig, root, valueStruct, err := n.signDuty(ctx, decidedValue, role, duty.Duty)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}

	// TODO - should we construct it better?
	if err := n.network.BroadcastSignature(&proto.SignedMessage{
		Message: &proto.Message{
			Lambda: identifier,
		},
		Signature: sig,
		SignerIds: []uint64{duty.NodeID},
	}); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasting partial signature post consensus")

	signatures, err := n.waitForSignatureCollection(logger, identifier, root, signaturesCount, duty.Committiee)

	// clean queue for messages, we don't need them anymore.
	n.queue.PurgeIndexedMessages(msgqueue.SigRoundIndexKey(identifier))

	if err != nil {
		return err
	}
	logger.Info("collected enough signature to reconstruct...", zap.Int("signatures", len(signatures)))

	// Reconstruct signatures
	if err := n.reconstructAndBroadcastSignature(ctx, logger, signatures, root, valueStruct, role, duty); err != nil {
		return errors.Wrap(err, "failed to reconstruct and broadcast signature")
	}
	logger.Info("Successfully submitted role!")
	return nil
}

func (n *ssvNode) comeToConsensusOnInputValue(
	ctx context.Context,
	logger *zap.Logger,
	prevIdentifier []byte,
	slot uint64,
	role beacon.Role,
	duty *slotqueue.Duty,
) (int, []byte, []byte, error) {
	var inputByts []byte
	var err error
	var valCheckInstance valcheck2.ValueCheck
	switch role {
	case beacon.RoleAttester:
		attData, err := n.beacon.GetAttestationData(ctx, slot, duty.Duty.GetCommitteeIndex())
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
	//	aggData, err := n.beacon.GetAggregationData(ctx, slot, duty.GetCommitteeIndex())
	//	if err != nil {
	//		return 0, nil, errors.Wrap(err, "failed to get aggregation data")
	//	}
	//
	//	inputValue.Data = &proto.InputValue_AggregationData{
	//		AggregationData: aggData,
	//	}
	//case beacon.RoleProposer:
	//	block, err := n.beacon.GetProposalData(ctx, slot)
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
	decided, signaturesCount, decidedByts := n.iBFT.StartInstance(ibft.StartOptions{
		Duty:         duty,
		Logger:       l,
		ValueCheck:   valCheckInstance,
		PrevInstance: prevIdentifier,
		Identifier:   identifier,
		Value:        inputByts,
	})

	if !decided {
		return 0, nil, nil, errors.New("ibft did not decide, not executing role")
	}
	return signaturesCount, decidedByts, identifier, nil
}

func (n *ssvNode) executeDuty(
	ctx context.Context,
	prevIdentifier []byte,
	slot uint64,
	duty *slotqueue.Duty,
) {
	logger := n.logger.With(zap.Time("start_time", n.getSlotStartTime(slot)),
		zap.Uint64("committee_index", duty.Duty.GetCommitteeIndex()),
		zap.Uint64("slot", slot))

	roles, err := n.beacon.RolesAt(ctx, slot, duty.Duty)
	if err != nil {
		logger.Error("failed to get roles for duty", zap.Error(err))
		return
	}

	for _, role := range roles {
		go func(role beacon.Role) {
			l := logger.With(zap.String("role", role.String()))

			signaturesCount, decidedValue, identifier, err := n.comeToConsensusOnInputValue(ctx, logger, prevIdentifier, slot, role, duty)
			if err != nil {
				logger.Error("could not come to consensus", zap.Error(err))
				return
			}

			// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
			logger.Info("GOT CONSENSUS", zap.Any("inputValueHex", hex.EncodeToString(decidedValue)))

			// Sign, aggregate and broadcast signature
			if err := n.postConsensusDutyExecution(
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

			//identfier = newId // TODO: Fix race condition
		}(role)
	}
}
