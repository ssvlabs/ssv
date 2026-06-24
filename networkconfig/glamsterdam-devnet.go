package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// GlamsterdamDevnetSSV is the SSV config for running against an ethpandaops Glamsterdam (Gloas /
// ePBS) devnet — seeded for devnet-5 (chain 7095321190, genesis 1780577940). The beacon config
// (genesis, fork schedule incl. GLOAS_FORK_EPOCH) is read from the BN at runtime; only the
// SSV-side values live here.
//
// Devnets are ephemeral and the SSV contracts are deployed per-network, so the three values
// marked TODO must be filled after the contract deploy + validator registration, and re-checked
// whenever the devnet is reset (or replaced by devnet-6+).
var GlamsterdamDevnetSSV = &SSV{
	Name:           "glamsterdam-devnet",
	DomainType:     spectypes.DomainType{0x0, 0x0, 0x09, 0x00},
	NextDomainType: spectypes.DomainType{0x0, 0x0, 0x09, 0x01},

	// TODO(e2e): SSV contract address + its deployment block, set after deploying on the devnet EL.
	RegistryContractAddr: ethcommon.Address{},
	RegistrySyncOffset:   big.NewInt(0),

	DiscoveryProtocolID: [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	// TODO(e2e): the 4 operators' bootnode ENRs (discovery seeds for the cluster).
	Bootnodes: nil,

	// TODO(e2e): approximate devnet validator count; feeds gossip message-rate scoring only.
	TotalEthereumValidators: 1000,

	// Boole is the SSV protocol baseline ePBS builds on — active from genesis on the devnet.
	Forks: SSVForks{Boole: 0},
}
