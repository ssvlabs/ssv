package validator

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// TestNewIdentifierFn_ForkDomain verifies that the identifier resolver wired into QBFT
// controllers by SetupCommitteeRunners/SetupRunners switches the SSV domain at the Boole
// fork boundary: heights before the fork slot carry DomainType (Alan), heights at/after
// it carry NextDomainType (Boole), with the executor ID and role preserved.
func TestNewIdentifierFn_ForkDomain(t *testing.T) {
	// Build a fork-spanning config explicitly (Boole at epoch 10) rather than aliasing the
	// package-global TestNetwork, whose Boole epoch can be flipped by SSV_TEST_BOOLE_FORK.
	forkEpoch := phase0.Epoch(10)
	ssvCfg := *networkconfig.TestNetwork.SSV
	ssvCfg.Forks = networkconfig.SSVForks{Boole: forkEpoch}
	cfg := &networkconfig.Network{Beacon: networkconfig.TestNetwork.Beacon, SSV: &ssvCfg}

	slotsPerEpoch := phase0.Slot(networkconfig.TestNetwork.SlotsPerEpoch)
	preForkHeight := specqbft.Height(phase0.Slot(forkEpoch)*slotsPerEpoch - 1)
	postForkHeight := specqbft.Height(phase0.Slot(forkEpoch) * slotsPerEpoch)

	committeeID := spectypes.CommitteeID{0x11, 0x22, 0x33}
	validatorPubKey := spectypes.ValidatorPK{0xaa, 0xbb, 0xcc}

	for _, tc := range []struct {
		name       string
		executorID []byte
		role       spectypes.RunnerRole
	}{
		{name: "committee executor", executorID: committeeID[:], role: spectypes.RoleCommittee},
		{name: "validator executor", executorID: validatorPubKey[:], role: spectypes.RoleProposer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identifierFn := newIdentifierFn(cfg, tc.executorID, tc.role)

			expectedPreFork := ssvtestingutils.NewMsgID(cfg.DomainType, tc.executorID, tc.role)
			expectedPostFork := ssvtestingutils.NewMsgID(cfg.NextDomainType, tc.executorID, tc.role)
			require.NotEqual(t, expectedPreFork, expectedPostFork,
				"sanity: pre- and post-fork identifiers must differ in domain")

			require.Equal(t, expectedPreFork[:], identifierFn(preForkHeight),
				"height before the fork slot must carry the Alan domain")
			require.Equal(t, expectedPostFork[:], identifierFn(postForkHeight),
				"height at/after the fork slot must carry the Boole domain")
		})
	}
}
