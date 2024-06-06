package exporter_message

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// NetworkID are intended to separate different SSV networks. A network can have many forks in it.
type NetworkID [1]byte

var (
	MainnetNetworkID = NetworkID{0x0}
	PrimusNetworkID  = NetworkID{0x1}
	ShifuNetworkID   = NetworkID{0x2}
	JatoNetworkID    = NetworkID{0x3}
	JatoV2NetworkID  = NetworkID{0x4}
)

// DomainType is a unique identifier for signatures, 2 identical pieces of data signed with different domains will result in different sigs
type DomainType [4]byte

// DomainTypes represent specific forks for specific chains, messages are signed with the domain type making 2 messages from different domains incompatible
// Historical Note:
// The fork version values for JatoTestnet and JatoV2Testnet are both set to 0x1 due to an error
// when these networks were initially introduced. This inconsistency does not align with the sequential
// versioning observed in other network and fork definitions. It's retained to maintain historical accuracy
// and to avoid any unforeseen issues that might arise from changing these established values.
// Future references and modifications should acknowledge this historical inconsistency.
var (
	GenesisMainnet = DomainType{0x0, 0x0, MainnetNetworkID.Byte(), 0x0}
	AlanMainnet    = DomainType{0x0, 0x0, MainnetNetworkID.Byte(), 0x1}

	PrimusTestnet   = DomainType{0x0, 0x0, PrimusNetworkID.Byte(), 0x0}
	ShifuTestnet    = DomainType{0x0, 0x0, ShifuNetworkID.Byte(), 0x0}
	ShifuV2Testnet  = DomainType{0x0, 0x0, ShifuNetworkID.Byte(), 0x1}
	JatoTestnet     = DomainType{0x0, 0x0, JatoNetworkID.Byte(), 0x1}   // Note the fork version value
	JatoV2Testnet   = DomainType{0x0, 0x0, JatoV2NetworkID.Byte(), 0x1} // Note the fork version value
	JatoAlanTestnet = DomainType{0x0, 0x0, JatoNetworkID.Byte(), 0x2}
)

// ForkData is a simple structure holding fork information for a specific chain (and its fork)
type ForkData struct {
	// Epoch in which the fork happened
	Epoch phase0.Epoch
	// Domain for the new fork
	Domain DomainType
}

func (n NetworkID) Byte() byte {
	return n[0]
}

// GetForksData return a sorted list of the forks of the network
func (n NetworkID) GetForksData() []*ForkData {
	switch n {
	case MainnetNetworkID:
		return mainnetForks()
	case PrimusNetworkID:
		return []*ForkData{{Epoch: 0, Domain: PrimusTestnet}}
	case JatoNetworkID:
		return []*ForkData{{Epoch: 0, Domain: JatoTestnet}}
	case JatoV2NetworkID:
		return []*ForkData{{Epoch: 0, Domain: JatoV2Testnet}}
	default:
		return []*ForkData{}
	}
}

// GetCurrentFork returns the ForkData with highest Epoch smaller or equal to "epoch"
func (n NetworkID) ForkAtEpoch(epoch phase0.Epoch) (*ForkData, error) {
	// Get list of forks
	forks := n.GetForksData()

	// If empty, raise error
	if len(forks) == 0 {
		return nil, errors.New("Fork list by GetForksData is empty. Unknown Network")
	}

	var current_fork *ForkData
	for _, fork := range forks {
		if fork.Epoch <= epoch {
			current_fork = fork
		}
	}
	return current_fork, nil
}

func (d DomainType) GetNetworkID() NetworkID {
	return NetworkID{d[2]}
}

// mainnetForks returns all forks for the mainnet chain
func mainnetForks() []*ForkData {
	return []*ForkData{
		{
			Epoch:  0,
			Domain: GenesisMainnet,
		},
		{
			// TODO: Update this when the mainnet fork is known
			Epoch:  0,
			Domain: AlanMainnet,
		},
	}
}
