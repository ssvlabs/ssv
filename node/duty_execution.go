package node

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/pkg/errors"
	"time"

	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"

	"github.com/bloxapp/ssv/beacon"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"
)

// postConsensusDutyExecution signs the eth2 duty after iBFT came to consensus,
// waits for others to sign, collect sigs, reconstruct and broadcast the reconstructed signature to the beacon chain
func (n *ssvNode) postConsensusDutyExecution(
	ctx context.Context,
	logger *zap.Logger,
	identifier []byte,
	inputValue *proto.InputValue,
	signaturesCount int,
	role beacon.Role,
	duty *ethpb.DutiesResponse_Duty,
) error {
	// sign input value and broadcast
	sig, root, err := n.signDuty(ctx, inputValue, role, duty)
	if err != nil {
		return errors.Wrap(err, "failed to sign input data")
	}

	// TODO - should we construct it better?
	if err := n.network.BroadcastSignature(&proto.SignedMessage{
		Message: &proto.Message{
			Lambda: identifier,
		},
		Signature: sig,
		SignerIds: []uint64{n.nodeID},
	}); err != nil {
		return errors.Wrap(err, "failed to broadcast signature")
	}
	logger.Info("broadcasted partial signature post consensus")

	// Collect signatures from other nodes
	// TODO - change signature count to min threshold
	signatures := make(map[uint64][]byte, signaturesCount)
	signedIndxes := make([]uint64, 0)
	done := false

	// start timeout
	go func() {
		<-time.After(n.signatureCollectionTimeout)
		err = errors.Errorf("timed out waiting for post consensus signatures, received %d", len(signedIndxes))
		done = true
	}()
	// loop through messages until timeout
	for {
		if done {
			break
		}
		if msg := n.queue.PopMessage(msgqueue.SigRoundIndexKey(identifier)); msg != nil {
			if len(msg.Msg.SignerIds) == 0 { // no signer, empty sig
				continue
			}
			if _, found := signatures[msg.Msg.SignerIds[0]]; found { // sig already exists
				continue
			}
			// verify sig
			if err := n.verifyPartialSignature(msg.Msg.Signature, root, msg.Msg.SignerIds[0]); err != nil {
				logger.Error("received invalid signature", zap.Error(err))
				continue
			}

			signatures[msg.Msg.SignerIds[0]] = msg.Msg.Signature
			signedIndxes = append(signedIndxes, msg.Msg.SignerIds[0])
			if len(signedIndxes) >= signaturesCount {
				done = true
				break
			}
		} else {
			time.Sleep(time.Millisecond * 100)
		}
	}

	if err != nil {
		return err
	}
	logger.Info("GOT ALL BROADCASTED SIGNATURES", zap.Int("signatures", len(signatures)))

	// Reconstruct signatures
	if err := n.reconstructAndBroadcastSignature(ctx, logger, signatures, root, inputValue, role, duty); err != nil {
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
	duty *ethpb.DutiesResponse_Duty,
) (int, *proto.InputValue, []byte, error) {
	inputValue := &proto.InputValue{}
	switch role {
	case beacon.RoleAttester:
		attData, err := n.beacon.GetAttestationData(ctx, slot, duty.GetCommitteeIndex())
		if err != nil {
			return 0, nil, nil, errors.Wrap(err, "failed to get attestation data")
		}

		inputValue.Data = &proto.InputValue_AttestationData{
			AttestationData: attData,
		}
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

	valBytes, err := json.Marshal(&inputValue)
	if err != nil {
		return 0, nil, nil, errors.Wrap(err, "failed to marshal input value")
	}

	identifier := []byte(fmt.Sprintf("%d_%s", slot, role.String()))
	decided, signaturesCount := n.iBFT.StartInstance(ibft.StartOptions{
		Logger:       l,
		Consensus:    bytesval.New(valBytes),
		PrevInstance: prevIdentifier,
		Identifier:   identifier,
		Value:        valBytes,
	})

	if !decided {
		return 0, nil, nil, errors.New("ibft did not decide, not executing role")
	}
	return signaturesCount, inputValue, identifier, nil
}

func (n *ssvNode) executeDuty(
	ctx context.Context,
	prevIdentifier []byte,
	slot uint64,
	duty *ethpb.DutiesResponse_Duty,
) {
	logger := n.logger.With(zap.Time("start_time", n.getSlotStartTime(slot)),
		zap.Uint64("committee_index", duty.GetCommitteeIndex()),
		zap.Uint64("slot", slot))

	roles, err := n.beacon.RolesAt(ctx, slot, duty)
	if err != nil {
		logger.Error("failed to get roles for duty", zap.Error(err))
		return
	}

	for _, role := range roles {
		go func(role beacon.Role) {
			l := logger.With(zap.String("role", role.String()))

			signaturesCount, inputValue, identifier, err := n.comeToConsensusOnInputValue(ctx, logger, prevIdentifier, slot, role, duty)
			if err != nil {
				logger.Error("could not come to consensus", zap.Error(err))
				return
			}

			// Here we ensure at least 2/3 instances got a val so we can sign data and broadcast signatures
			logger.Info("GOT CONSENSUS", zap.Any("inputValue", &inputValue))

			// Sign, aggregate and broadcast signature
			if err := n.postConsensusDutyExecution(
				ctx,
				l,
				identifier,
				inputValue,
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
