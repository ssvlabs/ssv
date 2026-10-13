package networkconfig

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

// TestBuiltinNetworkDomainsAreUnique guards the network-separation guarantee that DomainType
// provides: it feeds spectypes.NewMsgID and the handshake network-ID filter, so two networks (or
// two forks) sharing a domain become protocol-indistinguishable. Every built-in network's Alan
// (DomainType) and Boole (NextDomainType) domain must be globally unique across the whole matrix.
//
// This is the regression guard for the holesky/hoodi collision: holesky's naive Boole domain
// (Alan+1) aliased hoodi's live Alan domain, and the same for the -stage pair. Because the testnet
// domains are ad-hoc literals rather than derived from a NetworkID, nothing else catches a clash.
func TestBuiltinNetworkDomainsAreUnique(t *testing.T) {
	type owner struct {
		network string
		fork    string
	}
	seen := make(map[spectypes.DomainType]owner)

	claim := func(d spectypes.DomainType, network, fork string) {
		if prev, ok := seen[d]; ok {
			t.Fatalf("domain %#x is shared by %s/%s and %s/%s — networks must be domain-separated",
				d, prev.network, prev.fork, network, fork)
		}
		seen[d] = owner{network: network, fork: fork}
	}

	require.NotEmpty(t, supportedSSVConfigs)
	for name, cfg := range supportedSSVConfigs {
		claim(cfg.DomainType, name, "alan")
		claim(cfg.NextDomainType, name, "boole")
	}
}
