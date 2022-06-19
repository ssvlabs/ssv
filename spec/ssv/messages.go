package ssv

import (
	"crypto/sha256"
	"encoding/json"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
)

type PartialSigMsgType uint64

const (
	// PostConsensusPartialSig is a partial signature over a decided duty (attestation data, block, etc)
	PostConsensusPartialSig PartialSigMsgType = iota
	// RandaoPartialSig is a partial signature over randao reveal
	RandaoPartialSig
	// SelectionProofPartialSig is a partial signature for aggregator selection proof
	SelectionProofPartialSig
	// ContributionProofs is the partial selection proofs for sync committee contributions (it's an array of sigs)
	ContributionProofs
)

type PartialSignatureMessages []*PartialSignatureMessage

// Encode returns a msg encoded bytes or error
func (msgs *PartialSignatureMessages) Encode() ([]byte, error) {
	return json.Marshal(msgs)
}

// Decode returns error if decoding failed
func (msgs *PartialSignatureMessages) Decode(data []byte) error {
	return json.Unmarshal(data, msgs)
}

// GetRoot returns the root used for signing and verification
func (msgs PartialSignatureMessages) GetRoot() ([]byte, error) {
	marshaledRoot, err := msgs.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode PartialSignatureMessages")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

type PartialSignatureMetaData struct {
	ContributionSubCommitteeIndex uint64
}

// PartialSignatureMessage is a msg for partial beacon chain related signatures (like partial attestation, block, randao sigs)
type PartialSignatureMessage struct {
	Slot             spec.Slot // Slot represents the slot for which the partial BN signature is for
	PartialSignature []byte    // The beacon chain partial Signature for a duty
	SigningRoot      []byte    // the root signed in PartialSignature
	Signers          []types.OperatorID
	MetaData         *PartialSignatureMetaData
}

// Encode returns a msg encoded bytes or error
func (pcsm *PartialSignatureMessage) Encode() ([]byte, error) {
	return json.Marshal(pcsm)
}

// Decode returns error if decoding failed
func (pcsm *PartialSignatureMessage) Decode(data []byte) error {
	return json.Unmarshal(data, pcsm)
}

func (pcsm *PartialSignatureMessage) GetRoot() ([]byte, error) {
	marshaledRoot, err := pcsm.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "could not encode PartialSignatureMessage")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret[:], nil
}

func (pcsm *PartialSignatureMessage) Validate() error {
	if len(pcsm.PartialSignature) != 96 {
		return errors.New("PartialSignatureMessage sig invalid")
	}
	if len(pcsm.SigningRoot) != 32 {
		return errors.New("SigningRoot invalid")
	}
	if len(pcsm.Signers) != 1 {
		return errors.New("invalid PartialSignatureMessage signers")
	}
	return nil
}

// SignedPartialSignatureMessage is an operator's signature over PartialSignatureMessage
type SignedPartialSignatureMessage struct {
	Type      PartialSigMsgType
	Messages  PartialSignatureMessages
	Signature types.Signature
	Signers   []types.OperatorID
}

// Encode returns a msg encoded bytes or error
func (spcsm *SignedPartialSignatureMessage) Encode() ([]byte, error) {
	return json.Marshal(spcsm)
}

// Decode returns error if decoding failed
func (spcsm *SignedPartialSignatureMessage) Decode(data []byte) error {
	return json.Unmarshal(data, &spcsm)
}

func (spcsm *SignedPartialSignatureMessage) GetSignature() types.Signature {
	return spcsm.Signature
}

func (spcsm *SignedPartialSignatureMessage) GetSigners() []types.OperatorID {
	return spcsm.Signers
}

func (spcsm *SignedPartialSignatureMessage) GetRoot() ([]byte, error) {
	return spcsm.Messages.GetRoot()
}

func (spcsm *SignedPartialSignatureMessage) Aggregate(signedMsg types.MessageSignature) error {
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
	//	return errors.Wrap(err, "could not parse PartialSignature")
	//}
	//
	//sig2, err := blsSig(signedMsg.GetSignature())
	//if err != nil {
	//	return errors.Wrap(err, "could not parse PartialSignature")
	//}
	//
	//sig1.Add(sig2)
	//spcsm.Signature = sig1.Serialize()
	//spcsm.Signers = allSigners
	//return nil
	panic("implement")
}

// MatchedSigners returns true if the provided signer ids are equal to GetSignerIds() without order significance
func (spcsm *SignedPartialSignatureMessage) MatchedSigners(ids []types.OperatorID) bool {
	toMatchCnt := make(map[types.OperatorID]int)
	for _, id := range ids {
		toMatchCnt[id]++
	}

	foundCnt := make(map[types.OperatorID]int)
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

func blsSig(sig []byte) (*bls.Sign, error) {
	ret := &bls.Sign{}
	if err := ret.Deserialize(sig); err != nil {
		return nil, errors.Wrap(err, "could not covert PartialSignature byts to bls.sign")
	}
	return ret, nil
}

func (spcsm *SignedPartialSignatureMessage) Validate() error {
	if len(spcsm.Signature) != 96 {
		return errors.New("SignedPartialSignatureMessage sig invalid")
	}
	if len(spcsm.Signers) != 1 {
		return errors.New("no SignedPartialSignatureMessage signers")
	}
	if len(spcsm.Messages) == 0 {
		return errors.New("no SignedPartialSignatureMessage messages")
	}
	for _, m := range spcsm.Messages {
		if err := m.Validate(); err != nil {
			return errors.Wrap(err, "message invalid")
		}

		if spcsm.Type == ContributionProofs && m.MetaData == nil {
			return errors.New("metadata nil for contribution proofs")
		}
	}
	return nil
}
