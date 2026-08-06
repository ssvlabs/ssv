package validation

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// TestCommitteeRole locks the publish/receive symmetry for committee-backed roles.
// Every role that p2pNetwork.BroadcastAtSlot routes to the committee topic
// (RoleCommittee, RoleAggregatorCommittee) must resolve to the committee lookup on
// receive, otherwise those messages would be rejected as unknown validators.
func TestCommitteeRole(t *testing.T) {
	mv := &messageValidator{}

	require.True(t, mv.committeeRole(spectypes.RoleCommittee))
	require.True(t, mv.committeeRole(spectypes.RoleAggregatorCommittee))

	require.False(t, mv.committeeRole(spectypes.RoleProposer))
	require.False(t, mv.committeeRole(ssvtypes.RoleAggregator))
}
