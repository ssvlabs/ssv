package types

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecssv "github.com/bloxapp/ssv-spec-genesis/ssv"
	genesisspectypes "github.com/bloxapp/ssv-spec-genesis/types"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

type GenesisPartialSigContainer struct {
	*genesisspecssv.PartialSigContainer
}

func (g *GenesisPartialSigContainer) AddSignature(sigMsg PartialSignatureMessage) {
	msg, ok := sigMsg.(*genesisspectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	g.PartialSigContainer.AddSignature(msg)
}

func (g *GenesisPartialSigContainer) HasSigner(sigMsg PartialSignatureMessage) bool {
	msg, ok := sigMsg.(*genesisspectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	return g.PartialSigContainer.HasSigner(msg)
}

func (g *GenesisPartialSigContainer) HasQuorum(sigMsg PartialSignatureMessage) bool {
	msg, ok := sigMsg.(*genesisspectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	return g.PartialSigContainer.HasQuorum(msg)
}

func (g *GenesisPartialSigContainer) GetSignature(sigMsg PartialSignatureMessage) (spectypes.Signature, error) {
	msg, ok := sigMsg.(*genesisspectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	return g.PartialSigContainer.GetSignature(msg)
}

func (g *GenesisPartialSigContainer) Remove(validatorIndex phase0.ValidatorIndex, signer uint64, signingRoot [32]byte) {
	g.PartialSigContainer.Remove(signer, signingRoot)
}

func (g *GenesisPartialSigContainer) ReconstructSignature(root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	return g.PartialSigContainer.ReconstructSignature(root, validatorPubKey)
}

func (g *GenesisPartialSigContainer) GetSignatures(validatorIndex phase0.ValidatorIndex, root [32]byte) map[types.OperatorID][]byte {
	return g.PartialSigContainer.GetSignatures(root)
}

type AlanPartialSigContainer struct {
	*specssv.PartialSigContainer
}

func (g *AlanPartialSigContainer) AddSignature(sigMsg PartialSignatureMessage) {
	msg, ok := sigMsg.(*spectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	g.PartialSigContainer.AddSignature(msg)
}

func (g *AlanPartialSigContainer) HasSigner(sigMsg PartialSignatureMessage) bool {
	msg, ok := sigMsg.(*spectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	return g.PartialSigContainer.HasSigner(msg)
}

func (g *AlanPartialSigContainer) HasQuorum(sigMsg PartialSignatureMessage) bool {
	msg, ok := sigMsg.(*spectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	return g.PartialSigContainer.HasQuorum(msg)
}

func (g *AlanPartialSigContainer) GetSignature(sigMsg PartialSignatureMessage) (spectypes.Signature, error) {
	msg, ok := sigMsg.(*spectypes.PartialSignatureMessage)
	if !ok {
		panic("invalid type")
	}
	return g.PartialSigContainer.GetSignature(msg)
}

func (g *AlanPartialSigContainer) Remove(validatorIndex phase0.ValidatorIndex, signer uint64, signingRoot [32]byte) {
	g.PartialSigContainer.Remove(validatorIndex, signer, signingRoot)
}

func (g *AlanPartialSigContainer) ReconstructSignature(root [32]byte, validatorPubKey []byte, validatorIndex phase0.ValidatorIndex) ([]byte, error) {
	return g.PartialSigContainer.ReconstructSignature(root, validatorPubKey, validatorIndex)
}

func (g *AlanPartialSigContainer) GetSignatures(validatorIndex phase0.ValidatorIndex, root [32]byte) map[spectypes.OperatorID][]byte {
	return g.PartialSigContainer.GetSignatures(validatorIndex, root)
}
