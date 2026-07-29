package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"

	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type signing interface {
	// GetShareSigner returns a BeaconSigner instance
	GetShareSigner() ekm.BeaconSigner
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
	ProposerF    specqbft.ProposerF
	Network      protocolp2p.Network
	CutOffRound  specqbft.Round
}

// GetShareSigner returns a BeaconSigner instance
func (c *Config) GetShareSigner() ekm.BeaconSigner {
	return c.BeaconSigner
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
