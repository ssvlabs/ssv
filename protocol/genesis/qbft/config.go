package qbft

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	qbftstorage "github.com/ssvlabs/ssv/protocol/genesis/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/genesis/types"
)

type signing interface {
	// GetSigner returns a Signer instance
	GetSigner() genesisspectypes.SSVSigner
	// GetSignatureDomainType returns the Domain type used for signatures
	GetSignatureDomainType() genesisspectypes.DomainType
}

type IConfig interface {
	signing
	// GetValueCheckF returns value check function
	GetValueCheckF() genesisspecqbft.ProposedValueCheckF
	// GetProposerF returns func used to calculate proposer
	GetProposerF() func(state *types.State, round genesisspecqbft.Round) genesisspectypes.OperatorID
	// GetNetwork returns a p2p Network instance
	GetNetwork() genesisspecqbft.Network
	// GetStorage returns a storage instance
	GetStorage() qbftstorage.QBFTStore
	// GetTimer returns round timer
	GetTimer() roundtimer.Timer
	// VerifySignatures returns if signature is checked
	VerifySignatures() bool
	// GetOperatorID returns the operator ID
	GetOperatorID() genesisspectypes.OperatorID
}

type Config struct {
	Signer                genesisspectypes.SSVSigner
	SigningPK             []byte
	Domain                genesisspectypes.DomainType
	ValueCheckF           genesisspecqbft.ProposedValueCheckF
	ProposerF             func(state *types.State, round genesisspecqbft.Round) genesisspectypes.OperatorID
	Storage               qbftstorage.QBFTStore
	Network               genesisspecqbft.Network
	Timer                 roundtimer.Timer
	SignatureVerification bool
	OperatorID            genesisspectypes.OperatorID
}

// GetSigner returns a Signer instance
func (c *Config) GetSigner() genesisspectypes.SSVSigner {
	return c.Signer
}

// GetSigningPubKey returns the public key used to sign all QBFT messages
func (c *Config) GetSigningPubKey() []byte {
	return c.SigningPK
}

// GetSignatureDomainType returns the Domain type used for signatures
func (c *Config) GetSignatureDomainType() genesisspectypes.DomainType {
	return c.Domain
}

// GetValueCheckF returns value check instance
func (c *Config) GetValueCheckF() genesisspecqbft.ProposedValueCheckF {
	return c.ValueCheckF
}

// GetProposerF returns func used to calculate proposer
func (c *Config) GetProposerF() func(state *types.State, round genesisspecqbft.Round) genesisspectypes.OperatorID {
	return c.ProposerF
}

// GetNetwork returns a p2p Network instance
func (c *Config) GetNetwork() genesisspecqbft.Network {
	return c.Network
}

// GetStorage returns a storage instance
func (c *Config) GetStorage() qbftstorage.QBFTStore {
	return c.Storage
}

// GetTimer returns round timer
func (c *Config) GetTimer() roundtimer.Timer {
	return c.Timer
}

func (c *Config) VerifySignatures() bool {
	return c.SignatureVerification
}

func (c *Config) GetOperatorID() genesisspectypes.OperatorID {
	return c.OperatorID
}
