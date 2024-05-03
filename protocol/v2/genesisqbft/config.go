package genesisqbft

import (
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
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
	GetProposerF() genesisspecqbft.ProposerF
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
	Signer                genesisspectypes.SSVSigner
	SigningPK             []byte
	Domain                genesisspectypes.DomainType
	ValueCheckF           genesisspecqbft.ProposedValueCheckF
	ProposerF             genesisspecqbft.ProposerF
	Storage               qbftstorage.QBFTStore
	Network               genesisspecqbft.Network
	Timer                 roundtimer.Timer
	SignatureVerification bool
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
func (c *Config) GetProposerF() genesisspecqbft.ProposerF {
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
