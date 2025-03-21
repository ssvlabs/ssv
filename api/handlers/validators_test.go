package handlers

import (
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/api"
)

// mockShare creates a dummy SSVShare with the given operator IDs in the committee.
// It sets a dummy committee; other fields are left as zero values.
func mockShare(operatorIDs ...uint64) *types.SSVShare {
	operators := make([]*spectypes.ShareMember, len(operatorIDs))
	for i, id := range operatorIDs {
		operators[i] = &spectypes.ShareMember{
			Signer: id,
		}
	}
	return &types.SSVShare{
		Share: spectypes.Share{
			Committee: operators,
		},
	}
}

// TestByClusters verifies the byClusters filter helper against various cluster configurations.
func TestByClusters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		clusters  requestClusters
		contains  bool
		shares    []*types.SSVShare
		expectRes []bool
	}{
		{
			name:      "exact match",
			clusters:  requestClusters{{10, 20, 30}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{true, false},
		},
		{
			name:      "substring match",
			clusters:  requestClusters{{10, 20}},
			contains:  true,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{true, false},
		},
		{
			name:      "no match",
			clusters:  requestClusters{{40, 50}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{false, false},
		},
		{
			name:      "no match with contains",
			clusters:  requestClusters{{70, 80}},
			contains:  true,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{false, false},
		},
		{
			name:      "mismatch lengths",
			clusters:  requestClusters{{10, 20, 30}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(10, 20), mockShare(40, 50, 60)},
			expectRes: []bool{false, false},
		},
		{
			name:     "multiple clusters",
			clusters: requestClusters{{20, 30, 40}, {80, 90, 100}},
			contains: false,
			shares: []*types.SSVShare{
				mockShare(20, 30, 40),
				mockShare(40, 50, 60),
				mockShare(80, 90, 100),
				mockShare(70, 80, 90, 100),
				mockShare(60, 80, 100),
				mockShare(20, 30, 40),
			},
			expectRes: []bool{true, false, true, false, false, true},
		},
		{
			name:     "multiple clusters with contains",
			clusters: requestClusters{{20, 30, 40}, {80, 90, 100}},
			contains: true,
			shares: []*types.SSVShare{
				mockShare(10, 20, 30, 40, 50),
				mockShare(10, 30, 40),
				mockShare(40, 50, 60),
				mockShare(70, 80, 90, 100),
				mockShare(60, 80, 100),
				mockShare(20, 30, 40),
			},
			expectRes: []bool{true, false, false, true, false, true},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filter := byClusters(tc.clusters, tc.contains)
			for i, share := range tc.shares {
				res := filter(share)

				require.Equal(t, tc.expectRes[i], res, "for share %v", share)
			}
		})
	}
}

// TestByPubKeys verifies the byPubKeys filter helper using dummy hex values.
func TestByPubKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pubkeys []api.Hex
		share   *types.SSVShare
		want    bool
	}{
		{
			name:    "valid pubkey",
			pubkeys: []api.Hex{common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")},
			share: &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: spectypes.ValidatorPK(common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")),
				},
			},
			want: true,
		},
		{
			name:    "invalid pubkey",
			pubkeys: []api.Hex{common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")},
			share: &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: spectypes.ValidatorPK{0x1},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(strings.ToLower(tt.name), func(t *testing.T) {
			t.Parallel()

			validate := byPubKeys(tt.pubkeys)
			got := validate(tt.share)

			require.Equal(t, tt.want, got, "byPubKeys() returned %v; want %v", got, tt.want)
		})
	}
}

// TestByOperators verifies the byOperators filter helper.
func TestByOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operators []uint64
		share     *types.SSVShare
		want      bool
	}{
		{
			name:      "match operator",
			operators: []uint64{1},
			share:     mockShare(1),
			want:      true,
		},
		{
			name:      "no match",
			operators: []uint64{0},
			share:     mockShare(1),
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(strings.ToLower(tt.name), func(t *testing.T) {
			t.Parallel()

			validate := byOperators(tt.operators)
			got := validate(tt.share)

			require.Equal(t, tt.want, got, "byOperators() returned %v; want %v", got, tt.want)
		})
	}
}
