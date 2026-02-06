package qbft

import (
	specqbft "github.com/ssvlabs/ssv-spec/qbft"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
)

type IConfig interface {
	// GetProposerF returns func used to calculate proposer
	GetProposerF() specqbft.ProposerF
	// GetNetwork returns a p2p Network instance
	GetNetwork() specqbft.Network
	// GetTimer returns round timer
	GetTimer() roundtimer.Timer
	// GetCutOffRound returns the round cut off
	GetCutOffRound() specqbft.Round
}

type Config struct {
	ProposerF   specqbft.ProposerF
	Network     specqbft.Network
	Timer       roundtimer.Timer
	CutOffRound specqbft.Round
}

// GetProposerF returns func used to calculate proposer
func (c *Config) GetProposerF() specqbft.ProposerF {
	return c.ProposerF
}

// GetNetwork returns a p2p Network instance
func (c *Config) GetNetwork() specqbft.Network {
	return c.Network
}

// GetTimer returns round timer
func (c *Config) GetTimer() roundtimer.Timer {
	return c.Timer
}

func (c *Config) GetCutOffRound() specqbft.Round {
	return c.CutOffRound
}
