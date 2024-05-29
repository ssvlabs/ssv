package qbft

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer"
	qbftstorage "github.com/ssvlabs/ssv/protocol/genesis/qbft/storage"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
)

type signing interface {
	// GetShareSigner returns a ShareSigner instance
	GetShareSigner() genesisspectypes.ShareSigner
	// GetOperatorSigner returns an operator signer instance
	GetOperatorSigner() genesistypes.OperatorSigner
	// GetSignatureDomainType returns the Domain type used for signatures
	GetSignatureDomainType() genesisspectypes.DomainType
}

type IConfig interface {
	signing
	// GetValueCheckF returns value check function
	GetValueCheckF() genesisspecqbft.ProposedValueCheckF
	// GetProposerF returns func used to calculate proposer
	GetProposerF() func(state *genesistypes.State, round genesisspecqbft.Round) genesisspectypes.OperatorID
	// GetNetwork returns a p2p Network instance
	GetNetwork() genesisspecqbft.Network
	// GetStorage returns a storage instance
	GetStorage() qbftstorage.QBFTStore
	// GetTimer returns round timer
	GetTimer() roundtimer.Timer
	// VerifySignatures returns if signature is checked
	VerifySignatures() bool
}

type Config struct {
	ShareSigner           genesisspectypes.ShareSigner
	OperatorSigner        genesistypes.OperatorSigner
	SigningPK             []byte
	Domain                genesisspectypes.DomainType
	ValueCheckF           genesisspecqbft.ProposedValueCheckF
	ProposerF             func(state *genesistypes.State, round genesisspecqbft.Round) types.OperatorID
	Storage               qbftstorage.QBFTStore
	Network               genesisspecqbft.Network
	Timer                 roundtimer.Timer
	SignatureVerification bool
}

// GetShareSigner returns a ShareSigner instance
func (c *Config) GetShareSigner() genesisspectypes.ShareSigner {
	return c.ShareSigner
}

// GetOperatorSigner returns a OperatorSigner instance
func (c *Config) GetOperatorSigner() genesistypes.OperatorSigner {
	return c.OperatorSigner
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
func (c *Config) GetProposerF() func(state *genesistypes.State, round genesisspecqbft.Round) types.OperatorID {
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
