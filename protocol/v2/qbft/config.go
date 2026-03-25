package qbft

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
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
	// GetTimer returns round timer
	GetTimer() roundtimer.Timer
	// GetCutOffRound returns the round cut off
	GetCutOffRound() specqbft.Round
	// GetCommitteeBeaconVoteObserver returns a sink for committee BeaconVote input/proposal comparison observations.
	GetCommitteeBeaconVoteObserver() CommitteeBeaconVoteObserver
}

type CommitteeBeaconVoteObserver interface {
	ObserveCommitteeBeaconVoteComparison(ctx context.Context, observation CommitteeBeaconVoteComparisonObservation)
}

type CommitteeBeaconVoteComparisonObservation struct {
	Slot          phase0.Slot
	CommitteeID   spectypes.CommitteeID
	OperatorID    spectypes.OperatorID
	ProposerID    spectypes.OperatorID
	CommitteeSize uint64
	InputRoot     [32]byte
	ProposalRoot  [32]byte
	Match         bool
}

type Config struct {
	BeaconSigner ekm.BeaconSigner
	Domain       spectypes.DomainType
	ProposerF    specqbft.ProposerF
	Network      specqbft.Network
	Timer        roundtimer.Timer
	CutOffRound  specqbft.Round

	CommitteeBeaconVoteObserver CommitteeBeaconVoteObserver
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

// GetTimer returns round timer
func (c *Config) GetTimer() roundtimer.Timer {
	return c.Timer
}

func (c *Config) GetCutOffRound() specqbft.Round {
	return c.CutOffRound
}

func (c *Config) GetCommitteeBeaconVoteObserver() CommitteeBeaconVoteObserver {
	return c.CommitteeBeaconVoteObserver
}
