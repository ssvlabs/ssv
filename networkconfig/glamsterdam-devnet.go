package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// GlamsterdamDevnetSSV is the SSV config for running against an ethpandaops Glamsterdam (Gloas /
// ePBS) devnet — currently devnet-6 (chain 7052886157, genesis 1782386940 ≈ 2026-06-25; verified
// live 2026-06-30 at epoch ~1118, so GLOAS_FORK_EPOCH 30 is well in the past). The beacon config
// (genesis, fork schedule incl. GLOAS_FORK_EPOCH) is read from the BN at runtime; only the
// SSV-side values live here.
//
// Devnets are ephemeral and the SSV contracts are deployed per-network, so the values still marked
// TODO (the SSV contract address + sync offset, and the operator bootnode ENRs) must be filled
// after the contract deploy + operator/validator registration, and the whole block re-checked
// whenever the devnet is reset or replaced (devnet-5 → devnet-6 already happened; probe
// https://glamsterdam-devnet-N.ethpandaops.io/ to find the live one).
var GlamsterdamDevnetSSV = &SSV{
	Name:           "glamsterdam-devnet",
	DomainType:     spectypes.DomainType{0x0, 0x0, 0x09, 0x00},
	NextDomainType: spectypes.DomainType{0x0, 0x0, 0x09, 0x01},

	// TODO(e2e): SSV contract address + its deployment block, set after deploying on the devnet-6 EL.
	RegistryContractAddr: ethcommon.Address{},
	RegistrySyncOffset:   big.NewInt(0),

	DiscoveryProtocolID: [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	// TODO(e2e): the 4 operators' bootnode ENRs (discovery seeds for the cluster).
	Bootnodes: nil,

	// Approximate active-validator count (feeds gossip message-rate scoring only); ≈ the verified
	// active set on devnet-6 (3909) as of 2026-06-30 — refresh on devnet reset/replace.
	TotalEthereumValidators: 3909,

	// Boole is the SSV protocol baseline ePBS builds on — active from genesis on the devnet.
	Forks: SSVForks{Boole: 0},
}
