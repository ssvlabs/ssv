package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
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
	GetNetwork() protocolp2p.Network
	// GetCutOffRound returns the round cut off
	GetCutOffRound() specqbft.Round
}

type Config struct {
	BeaconSigner ekm.BeaconSigner
	Domain       spectypes.DomainType
	ProposerF    specqbft.ProposerF
	Network      protocolp2p.Network
	CutOffRound  specqbft.Round
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
func (c *Config) GetNetwork() protocolp2p.Network {
	return c.Network
}

func (c *Config) GetCutOffRound() specqbft.Round {
	return c.CutOffRound
}
