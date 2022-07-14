package message

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// ValidatePartialSigMsg validates the signed partial signature message | NOTE: using this code and not from spec until duty runner is implemented
func ValidatePartialSigMsg(signedMsg *ssv.SignedPartialSignatureMessage, committee []*types.Operator, slot spec.Slot) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "could not validate SignedPartialSignatureMessage")
	}

	if err := signedMsg.GetSignature().VerifyByOperators(signedMsg, types.PrimusTestnet, types.PartialSignatureType, committee); err != nil {
		return errors.Wrap(err, "could not verify PartialSignature by the provided operators")
	}

	for _, msg := range signedMsg.Messages {
		if slot != msg.Slot {
			return errors.New("wrong slot")
		}

		if err := verifyBeaconPartialSignature(msg, committee); err != nil {
			return errors.Wrap(err, "could not verify beacon partial Signature")
		}
	}

	return nil
}

func verifyBeaconPartialSignature(msg *ssv.PartialSignatureMessage, committee []*types.Operator) error {
	if len(msg.Signers) != 1 {
		return errors.New("PartialSignatureMessage allows 1 signer")
	}

	signer := msg.Signers[0]
	signature := msg.PartialSignature
	root := msg.SigningRoot

	for _, n := range committee {
		if n.GetID() == signer {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(n.GetPublicKey()); err != nil {
				return errors.Wrap(err, "could not deserialized pk")
			}
			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return errors.Wrap(err, "could not deserialized Signature")
			}

			// protect nil root
			root = ensureRoot(root)
			// verify
			if !sig.VerifyByte(pk, root) {
				return errors.Errorf("could not verify Signature from iBFT member %d", signer)
			}
			return nil
		}
	}
	return errors.New("beacon partial Signature signer not found")
}

// ensureRoot ensures that SigningRoot will have sufficient allocated memory
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
