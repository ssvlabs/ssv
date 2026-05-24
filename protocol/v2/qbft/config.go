package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type signing interface {
	// GetShareSigner returns a BeaconSigner instance
	GetShareSigner() ekm.BeaconSigner
	// GetSignatureDomainType returns the Domain type used for signatures
	GetSignatureDomainType() spectypes.DomainType
}

type IConfig interface {
	signing
	// GetProposerF returns func used to calculate proposer
	GetProposerF() specqbft.ProposerF
	// GetNetwork returns a p2p Network instance
	GetNetwork() specqbft.Network
	// GetCutOffRound returns the round cut off
	GetCutOffRound() specqbft.Round
}

// SignatureVerifier verifies a SignedSSVMessage's signatures against a
// committee's operators. It has the same shape and contract as
// spectypes.Verify and MUST return exactly what spectypes.Verify would — it is
// only a place to substitute a faster (e.g. memoizing) implementation, never a
// policy change.
type SignatureVerifier func(msg *spectypes.SignedSSVMessage, operators []*spectypes.Operator) error

type Config struct {
	BeaconSigner ekm.BeaconSigner
	Domain       spectypes.DomainType
	ProposerF    specqbft.ProposerF
	Network      specqbft.Network
	CutOffRound  specqbft.Round
	// SignatureVerifier, when non-nil, replaces spectypes.Verify for consensus
	// message signature checks in the Instance. Production leaves it nil, so the
	// default (spectypes.Verify) path is unchanged. The consensustest stress
	// harness injects a memoizing verifier: the sweep re-verifies the same
	// signatures millions of times (identical proposed value / identifier /
	// height across iterations, plus QBFT's O(n²) re-validation of round-change
	// justifications), and caching turns those repeats from RSA modexp into a
	// map lookup.
	SignatureVerifier SignatureVerifier
}

// GetShareSigner returns a BeaconSigner instance
func (c *Config) GetShareSigner() ekm.BeaconSigner {
	return c.BeaconSigner
}

// GetSignatureDomainType returns the Domain type used for signatures
func (c *Config) GetSignatureDomainType() spectypes.DomainType {
	return c.Domain
}

// GetProposerF returns func used to calculate proposer
func (c *Config) GetProposerF() specqbft.ProposerF {
	return c.ProposerF
}

// GetNetwork returns a p2p Network instance
func (c *Config) GetNetwork() specqbft.Network {
	return c.Network
}

func (c *Config) GetCutOffRound() specqbft.Round {
	return c.CutOffRound
}

// GetSignatureVerifier returns the optional signature-verification override
// (nil = use spectypes.Verify). Not part of IConfig: the Instance discovers it
// via a type assertion so configs that don't supply one keep the default path.
func (c *Config) GetSignatureVerifier() SignatureVerifier {
	return c.SignatureVerifier
}
