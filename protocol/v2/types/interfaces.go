package types

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
)

type PartialSignatureMessages interface {
	Serialize() ([]byte, error)
	Deserialize(data []byte) error
	Validate() error
	GetSigner() types.OperatorID
	GetMessages() []PartialSignatureMessage
	GetSlot() phase0.Slot
	GetRoot() [32]byte
}

type SignedMessage interface {
	Encode() ([]byte, error)
	Decode(data []byte) error
	GetRoot() ([32]byte, error)
	GetOperatorIDs() []types.OperatorID
	GetHeight() qbft.Height
}

type PartialSignatureMessage interface {
	GetPartialSignature() types.Signature
	GetSigningRoot() [32]byte
	GetSigner() types.OperatorID
	GetValidatorIndex() phase0.ValidatorIndex
}

type PartialSigContainer interface {
	AddSignature(sigMsg PartialSignatureMessage)
	HasSigner(sigMsg PartialSignatureMessage) bool
	HasQuorum(sigMsg PartialSignatureMessage) bool
	GetSignature(sigMsg PartialSignatureMessage) (types.Signature, error)
	GetSignatures(validatorIndex phase0.ValidatorIndex, root [32]byte) map[types.OperatorID][]byte
	Remove(validatorIndex phase0.ValidatorIndex, signer uint64, signingRoot [32]byte)
	ReconstructSignature(root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error)
}
