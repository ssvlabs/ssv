package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	protocolp2p "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

// TODO: (Alan) revert back to using spec Network once they merge in the msgID passing with cluster consensus branch.
type FutureSpecNetwork interface {
	protocolp2p.Broadcaster
}

type signing interface {
	// GetShareSigner returns a BeaconSigner instance
	GetShareSigner() spectypes.BeaconSigner
	// GetOperatorSigner returns an operator signer instance
	GetOperatorSigner() spectypes.OperatorSigner
	// GetSignatureDomainType returns the Domain type used for signatures
	GetSignatureDomainType() spectypes.DomainType
}

type IConfig interface {
	signing
	// GetValueCheckF returns value check function
	GetValueCheckF() specqbft.ProposedValueCheckF
	// GetProposerF returns func used to calculate proposer
	GetProposerF() specqbft.ProposerF
	// GetNetwork returns a p2p Network instance
	GetNetwork() FutureSpecNetwork
	// GetStorage returns a storage instance
	GetStorage() qbftstorage.QBFTStore
	// GetTimer returns round timer
	GetTimer() roundtimer.Timer
	// VerifySignatures returns if signature is checked
	VerifySignatures() bool
	// GetSignatureVerifier returns the signature verifier for operator signatures
	GetSignatureVerifier() spectypes.SignatureVerifier
	// GetRoundCutOff returns the round cut off
	GetCutOffRound() int
}

type Config struct {
	BeaconSigner          spectypes.BeaconSigner
	OperatorSigner        spectypes.OperatorSigner
	SigningPK             []byte
	Domain                spectypes.DomainType
	ValueCheckF           specqbft.ProposedValueCheckF
	ProposerF             specqbft.ProposerF
	Storage               qbftstorage.QBFTStore
	Network               FutureSpecNetwork
	Timer                 roundtimer.Timer
	SignatureVerification bool
	SignatureVerifier     spectypes.SignatureVerifier
	CutOffRound           int
}

// GetShareSigner returns a BeaconSigner instance
func (c *Config) GetShareSigner() spectypes.BeaconSigner {
	return c.BeaconSigner
}

// GetOperatorSigner returns a OperatorSigner instance
func (c *Config) GetOperatorSigner() spectypes.OperatorSigner {
	return c.OperatorSigner
}

// GetSigningPubKey returns the public key used to sign all QBFT messages
func (c *Config) GetSigningPubKey() []byte {
	return c.SigningPK
}

// GetSignatureDomainType returns the Domain type used for signatures
func (c *Config) GetSignatureDomainType() spectypes.DomainType {
	return c.Domain
}

// GetValueCheckF returns value check instance
func (c *Config) GetValueCheckF() specqbft.ProposedValueCheckF {
	return c.ValueCheckF
}

// GetProposerF returns func used to calculate proposer
func (c *Config) GetProposerF() specqbft.ProposerF {
	return c.ProposerF
}

// GetNetwork returns a p2p Network instance
func (c *Config) GetNetwork() FutureSpecNetwork {
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

func (c *Config) GetCutOffRound() int {
	return c.CutOffRound
}

func (c *Config) VerifySignatures() bool {
	return c.SignatureVerification
}

func (c *Config) GetSignatureVerifier() spectypes.SignatureVerifier {
	return c.SignatureVerifier
}
