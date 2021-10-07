package validator

import (
	"encoding/base64"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func (v *Validator) verifyPartialSignature(signature []byte, root []byte, ibftID uint64, committiee map[uint64]*proto.Node) error {
	if val, found := committiee[ibftID]; found {
		pk := &bls.PublicKey{}
		if err := pk.Deserialize(val.Pk); err != nil {
			return errors.Wrap(err, "could not deserialized pk")
		}
		sig := &bls.Sign{}
		if err := sig.Deserialize(signature); err != nil {
			return errors.Wrap(err, "could not deserialized signature")
		}

		// protect nil root
		root = ensureRoot(root)
		// verify
		if !sig.VerifyByte(pk, root) {
			return errors.Errorf("could not verify signature from iBFT member %d", ibftID)
		}
		return nil
	}
	return errors.Errorf("could not find iBFT member %d", ibftID)
}

// signDuty signs the duty after iBFT came to consensus
func (v *Validator) signDuty(decidedValue []byte, duty *beacon.Duty, shareKey *bls.SecretKey) ([]byte, []byte, *beacon.DutyData, error) {
	// sign input value
	var sig []byte
	var root []byte
	retValueStruct := &beacon.DutyData{}
	var err error
	switch duty.Type {
	case beacon.RoleTypeAttester:
		s := &spec.AttestationData{}
		if err := s.UnmarshalSSZ(decidedValue); err != nil {
			return nil, nil, nil, errors.Wrap(err, "failed to marshal attestation")
		}
		signedAttestation, r, e := v.beacon.SignAttestation(s, duty, shareKey)
		if e != nil {
			return nil, nil, nil, errors.Wrap(err, "failed to sign attestation")
		}
		sg := &beacon.InputValueAttestation{Attestation: signedAttestation}
		retValueStruct.SignedData = sg
		retValueStruct.GetAttestation().Signature = signedAttestation.Signature
		retValueStruct.GetAttestation().AggregationBits = signedAttestation.AggregationBits
		sig = signedAttestation.Signature[:]
		root = ensureRoot(r)
	//case beacon.RoleTypeAggregator:
	//	s := &proto.InputValue_Aggregation{}
	//	if err := json.Unmarshal(decidedValue, s); err != nil {
	//		return nil, nil, nil, errors.Wrap(err, "failed to marshal aggregator")
	//	}
	//	signedAggregation, e := v.beacon.SignAggregation(ctx, s.Aggregation.Message, v.Share.ShareKey)
	//	if e != nil{
	//		return nil, nil, nil, errors.Wrap(err, "failed to sign attestation")
	//	}
	//	retValueStruct.SignedData = s
	//	retValueStruct.GetAggregation().Signature = signedAggregation.Signature
	//	retValueStruct.GetAggregation().Message = signedAggregation.Message
	//	err = e
	//	sig = signedAggregation.GetSignature()
	//case beacon.RoleTypeProposer:
	//	s := &proto.InputValue_Block{}
	//	if err := json.Unmarshal(decidedValue, s); err != nil {
	//		return nil, nil, nil, errors.Wrap(err, "failed to marshal aggregator")
	//	}
	//
	//	signedProposal, e := v.beacon.SignProposal(ctx, nil, s.Block.Block, v.Share.ShareKey)
	//	if e != nil{
	//		return nil, nil, nil, errors.Wrap(err, "failed to sign attestation")
	//	}
	//
	//	retValueStruct.SignedData = s
	//	retValueStruct.GetBlock().Signature = signedProposal.Signature
	//	retValueStruct.GetBlock().Block = signedProposal.Block
	//	err = e
	//	sig = signedProposal.GetSignature()
	default:
		return nil, nil, nil, errors.New("unsupported role, can't sign")
	}
	return sig, root, retValueStruct, err
}

// reconstructAndBroadcastSignature reconstructs the received signatures from other
// nodes and broadcasts the reconstructed signature to the beacon-chain
func (v *Validator) reconstructAndBroadcastSignature(logger *zap.Logger, signatures map[uint64][]byte, root []byte, inputValue *beacon.DutyData, duty *beacon.Duty) error {
	// Reconstruct signatures
	signature, err := threshold.ReconstructSignatures(signatures)
	if err != nil {
		return errors.Wrap(err, "failed to reconstruct signatures")
	}
	// verify reconstructed sig
	if res := signature.VerifyByte(v.Share.PublicKey, root); !res {
		return errors.New("could not reconstruct a valid signature")
	}

	logger.Info("signatures successfully reconstructed", zap.String("signature", base64.StdEncoding.EncodeToString(signature.Serialize())), zap.Int("signature count", len(signatures)))

	// Submit validation to beacon node
	switch duty.Type {
	case beacon.RoleTypeAttester:
		logger.Debug("submitting attestation")
		blsSig := spec.BLSSignature{}
		copy(blsSig[:], signature.Serialize()[:])
		inputValue.GetAttestation().Signature = blsSig
		if err := v.beacon.SubmitAttestation(inputValue.GetAttestation()); err != nil {
			return errors.Wrap(err, "failed to broadcast attestation")
		}
	//case beacon.RoleTypeAggregator:
	//	inputValue.GetAggregation().Signature = signature.Serialize()
	//	if err := v.beacon.SubmitAggregation(ctx, inputValue.GetAggregation()); err != nil {
	//		return errors.Wrap(err, "failed to broadcast aggregation")
	//	}
	//case beacon.RoleTypeProposer:
	//	inputValue.GetBlock().Signature = signature.Serialize()
	//	if err := v.beacon.SubmitProposal(ctx, inputValue.GetBlock()); err != nil {
	//		return errors.Wrap(err, "failed to broadcast proposal")
	//	}
	default:
		return errors.New("role is undefined, can't reconstruct signature")
	}
	return nil
}

// ensureRoot ensures that root will have sufficient allocated memory
// otherwise we get panic from bls:
// github.com/herumi/bls-eth-go-binary/bls.(*Sign).VerifyByte:738
func ensureRoot(root []byte) []byte {
	n := len(root)
	if n == 0 {
		n = 1
	}
	tmp := make([]byte, n)
	copy(tmp[:], root[:])
	return tmp[:]
}
