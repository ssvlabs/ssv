package handlers

import (
	"testing"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func mockShare(operatorIDs ...uint64) *types.SSVShare {
	operators := make([]*spectypes.Operator, len(operatorIDs))
	for i, id := range operatorIDs {
		operators[i] = &spectypes.Operator{
			OperatorID: id,
		}
	}
	return &types.SSVShare{
		Share: spectypes.Share{
			Committee: operators,
		},
	}
}

func TestByClusters(t *testing.T) {
	testCases := []struct {
		name      string
		clusters  requestClusters
		contains  bool
		shares    []*types.SSVShare
		expectRes []bool
	}{
		{
			name:      "Exact Match",
			clusters:  requestClusters{{10, 20, 30}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{true, false},
		},
		{
			name:      "Substring Match",
			clusters:  requestClusters{{10, 20}},
			contains:  true,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{true, false},
		},
		{
			name:      "No Match",
			clusters:  requestClusters{{40, 50}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{false, false},
		},
		{
			name:      "No Match With Contains",
			clusters:  requestClusters{{70, 80}},
			contains:  true,
			shares:    []*types.SSVShare{mockShare(10, 20, 30), mockShare(40, 50, 60)},
			expectRes: []bool{false, false},
		},
		{
			name:      "Mismatch Lengths",
			clusters:  requestClusters{{10, 20, 30}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(10, 20), mockShare(40, 50, 60)},
			expectRes: []bool{false, false},
		},
		{
			name:      "Multiple Clusters",
			clusters:  requestClusters{{20, 30, 40}, {80, 90, 100}},
			contains:  false,
			shares:    []*types.SSVShare{mockShare(20, 30, 40), mockShare(40, 50, 60), mockShare(80, 90, 100), mockShare(70, 80, 90, 100), mockShare(60, 80, 100), mockShare(20, 30, 40)},
			expectRes: []bool{true, false, true, false, false, true},
		},
		{
			name:      "Multiple Clusters With Contains",
			clusters:  requestClusters{{20, 30, 40}, {80, 90, 100}},
			contains:  true,
			shares:    []*types.SSVShare{mockShare(10, 20, 30, 40, 50), mockShare(10, 30, 40), mockShare(40, 50, 60), mockShare(70, 80, 90, 100), mockShare(60, 80, 100), mockShare(20, 30, 40)},
			expectRes: []bool{true, false, false, true, false, true},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filter := byClusters(tc.clusters, tc.contains)
			for i, share := range tc.shares {
				res := filter(share)
				if res != tc.expectRes[i] {
					t.Errorf("Expected %v but got %v for share %v", tc.expectRes[i], res, share)
				}
			}
		})
	}
}
