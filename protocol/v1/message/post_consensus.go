package message

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

// PostConsensusMessage is the structure used for post consensus messages
type PostConsensusMessage struct {
	Height          Height
	DutySignature   []byte // The beacon chain partial Signature for a duty
	DutySigningRoot []byte // the root signed in DutySignature
	Signers         []OperatorID
}

// Encode returns a msg encoded bytes or error
func (pcsm *PostConsensusMessage) Encode() ([]byte, error) {
	return json.Marshal(pcsm)
}

// Decode returns error if decoding failed
func (pcsm *PostConsensusMessage) Decode(data []byte) error {
	return json.Unmarshal(data, pcsm)
}

// GetRoot returns the root of the message
func (pcsm *PostConsensusMessage) GetRoot(forkVersion string) ([]byte, error) {
	// using string version for checking in order to prevent cycle dependency
	/*if forkVersion == "v0" { TODo currently we do not signing postConsensus msgs so no need to change root
		return v0.PostConsensusToV0ProtoMessage(pcsm)
	}*/

	marshaledRoot, err := pcsm.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode PostConsensusMessage")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

// SignedPostConsensusMessage is the structure used for signed post consensus messages
type SignedPostConsensusMessage struct {
	Message   *PostConsensusMessage
	Signature Signature
	Signers   []OperatorID
}

// Encode returns a msg encoded bytes or error
func (spcsm *SignedPostConsensusMessage) Encode() ([]byte, error) {
	return json.Marshal(spcsm)
}

// Decode returns error if decoding failed
func (spcsm *SignedPostConsensusMessage) Decode(data []byte) error {
	return json.Unmarshal(data, spcsm)
}

// GetSignature returns the message signature
func (spcsm *SignedPostConsensusMessage) GetSignature() Signature {
	return spcsm.Signature
}

// GetSigners returns the message signers
func (spcsm *SignedPostConsensusMessage) GetSigners() []OperatorID {
	return spcsm.Signers
}

// GetRoot returns the signature root
func (spcsm *SignedPostConsensusMessage) GetRoot(forkVersion string) ([]byte, error) {
	return spcsm.Message.GetRoot(forkVersion)
}

// Aggregate aggregates signatures
func (spcsm *SignedPostConsensusMessage) Aggregate(signedMsg ...MsgSignature) error {
	//if !bytes.Equal(spcsm.GetRoot(), signedMsg.GetRoot()) {
	//	return errors.New("can't aggregate msgs with different roots")
	//}
	//
	//// verify no matching Signers
	//for _, signerID := range spcsm.Signers {
	//	for _, toMatchID := range signedMsg.GetSigners() {
	//		if signerID == toMatchID {
	//			return errors.New("signer IDs partially/ fully match")
	//		}
	//	}
	//}
	//
	//allSigners := append(spcsm.Signers, signedMsg.GetSigners()...)
	//
	//// verify and aggregate
	//sig1, err := blsSig(spcsm.Signature)
	//if err != nil {
	//	return errors.Wrap(err, "could not parse DutySignature")
	//}
	//
	//sig2, err := blsSig(signedMsg.GetSignature())
	//if err != nil {
	//	return errors.Wrap(err, "could not parse DutySignature")
	//}
	//
	//sig1.Add(sig2)
	//spcsm.Signature = sig1.Serialize()
	//spcsm.Signers = allSigners
	//return nil
	panic("implement")
}

// MatchedSigners returns true if the provided signer ids are equal to GetSignerIds() without order significance
func (spcsm *SignedPostConsensusMessage) MatchedSigners(ids []OperatorID) bool {
	toMatchCnt := make(map[OperatorID]int)
	for _, id := range ids {
		toMatchCnt[id]++
	}

	foundCnt := make(map[OperatorID]int)
	for _, id := range spcsm.GetSigners() {
		foundCnt[id]++
	}

	for id, cnt := range toMatchCnt {
		if cnt != foundCnt[id] {
			return false
		}
	}
	return true
}

//
//func blsSig(sig []byte) (*bls.Sign, error) {
//	ret := &bls.Sign{}
//	if err := ret.Deserialize(sig); err != nil {
//		return nil, errors.Wrap(err, "could not covert DutySignature byts to bls.sign")
//	}
//	return ret, nil
//}

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
