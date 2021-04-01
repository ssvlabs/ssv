package node

import (
	"context"
	"encoding/base64"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"go.uber.org/zap"
	"log"
)

// signDuty signs the duty after iBFT came to consensus
func (n *ssvNode) signDuty(
	ctx context.Context,
	inputValue *proto.InputValue,
	role beacon.Role,
	duty *ethpb.DutiesResponse_Duty,
) ([]byte, error){
	// sign input value
	var sig []byte
	var err error
	switch role {
	case beacon.RoleAttester:
		signedAttestation, e := n.beacon.SignAttestation(ctx, inputValue.GetAttestationData(), duty.GetValidatorIndex(), duty.GetCommittee())
		inputValue.SignedData = &proto.InputValue_Attestation{
			Attestation: signedAttestation,
		}
		err = e
		sig = signedAttestation.GetSignature()
	case beacon.RoleAggregator:
		signedAggregation, e := n.beacon.SignAggregation(ctx, inputValue.GetAggregationData())
		inputValue.SignedData = &proto.InputValue_Aggregation{
			Aggregation: signedAggregation,
		}
		err = e
		sig = signedAggregation.GetSignature()
	case beacon.RoleProposer:
		signedProposal, e := n.beacon.SignProposal(ctx, inputValue.GetBeaconBlock())
		inputValue.SignedData = &proto.InputValue_Block{
			Block: signedProposal,
		}
		err = e
		sig = signedProposal.GetSignature()
	}
	return sig, err
}

// reconstructAndBroadcastSignature reconstructs the received signatures from other
// nodes and broadcasts the reconstructed signature to the beacon-chain
func (n *ssvNode) reconstructAndBroadcastSignature(
	ctx context.Context,
	logger *zap.Logger,
	signatures map[uint64][]byte,
	inputValue *proto.InputValue,
	role beacon.Role,
	duty *ethpb.DutiesResponse_Duty,
) error {
	logger.Info("GOT ALL BROADCASTED SIGNATURES", zap.Int("signatures", len(signatures)))

	// Reconstruct signatures
	signature, err := threshold.ReconstructSignatures(signatures)
	if err != nil {
		return errors.Wrap(err, "failed to reconstruct signatures")
	}
	logger.Info("signatures successfully reconstructed", zap.String("signature", base64.StdEncoding.EncodeToString(signature)))

	// Submit validation to beacon node
	switch role {
	case beacon.RoleAttester:
		logger.Info("submitting attestation")
		inputValue.GetAttestation().Signature = signature
		log.Printf("%s, %d\n", inputValue.GetAttestation(), duty.GetValidatorIndex())
		if err := n.beacon.SubmitAttestation(ctx, inputValue.GetAttestation(), duty.GetValidatorIndex()); err != nil {
			return errors.Wrap(err, "failed to broadcast attestation")
		}
	case beacon.RoleAggregator:
		inputValue.GetAggregation().Signature = signature
		if err := n.beacon.SubmitAggregation(ctx, inputValue.GetAggregation()); err != nil {
			return errors.Wrap(err, "failed to broadcast aggregation")
		}
	case beacon.RoleProposer:
		inputValue.GetBlock().Signature = signature
		if err := n.beacon.SubmitProposal(ctx, inputValue.GetBlock()); err != nil {
			return errors.Wrap(err, "failed to broadcast proposal")
		}
	default:
		return errors.New("role is undefined")
	}
	return nil
}