package handlers

import (
	"reflect"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/api"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/types"
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

func TestByPubKeys(t *testing.T) {
	tests := []struct {
		name    string
		pubkeys []api.Hex
		share   *types.SSVShare
		want    bool
	}{
		{
			name:    "ok",
			pubkeys: []api.Hex{ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")},
			share: &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"),
				},
			},
			want: true,
		},
		{
			name:    "error",
			pubkeys: []api.Hex{ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")},
			share: &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: []byte{0},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validate := byPubKeys(tt.pubkeys)
			if got := validate(tt.share); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("byPubKeys() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestByOperators(t *testing.T) {
	tests := []struct {
		name      string
		operators []uint64
		share     *types.SSVShare
		want      bool
	}{
		{
			name:      "ok",
			operators: []uint64{1},
			share:     mockShare(1),
			want:      true,
		},
		{
			name:      "err",
			operators: []uint64{0},
			share:     mockShare(1),
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validate := byOperators(tt.operators)
			if got := validate(tt.share); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("byOperators() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestByIndices(t *testing.T) {
	tests := []struct {
		name    string
		indices []uint64
		share   *types.SSVShare
		want    bool
	}{
		{
			name:    "ok",
			indices: []uint64{1},
			share: &types.SSVShare{
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Index: phase0.ValidatorIndex(1),
					},
				},
			},
			want: true,
		},
		{
			name:    "err",
			indices: []uint64{0},
			share: &types.SSVShare{
				Metadata: types.Metadata{
					BeaconMetadata: &beacon.ValidatorMetadata{
						Index: phase0.ValidatorIndex(1),
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validate := byIndices(tt.indices)
			if got := validate(tt.share); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("byIndices() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestByOwners(t *testing.T) {
	tests := []struct {
		name   string
		owners []api.Hex
		share  *types.SSVShare
		want   bool
	}{
		{
			name:   "ok",
			owners: []api.Hex{ethcommon.HexToAddress("0x1").Bytes()},
			share: &types.SSVShare{
				Metadata: types.Metadata{
					OwnerAddress: ethcommon.HexToAddress("0x1"),
				},
			},
			want: true,
		},
		{
			name:   "err",
			owners: []api.Hex{ethcommon.HexToAddress("0x0").Bytes()},
			share: &types.SSVShare{
				Metadata: types.Metadata{
					OwnerAddress: ethcommon.HexToAddress("0x1"),
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validate := byOwners(tt.owners)
			if got := validate(tt.share); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("byOwners() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidatorFromShare(t *testing.T) {
	tests := []struct {
		name  string
		share *types.SSVShare
		want  validatorJSON
	}{
		{
			name: "ok",
			share: &types.SSVShare{
				Share: spectypes.Share{
					ValidatorPubKey: ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"),
					Committee: []*spectypes.Operator{
						{
							OperatorID: 1,
							PubKey:     ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"),
						},
					},
				},
				Metadata: types.Metadata{
					OwnerAddress: ethcommon.HexToAddress("0x1"),
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Balance:         1000,
						Status:          1,
						Index:           1,
						ActivationEpoch: 1,
					},
				},
			},
			want: validatorJSON{
				PubKey:    api.Hex(ethcommon.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")),
				Owner:     api.Hex(ethcommon.HexToAddress("0x1").Bytes()),
				Committee: []spectypes.OperatorID{1},
				Index:     1,
				Status:    "pending_initialized",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want.PubKey, validatorFromShare(tt.share).PubKey)
			require.Equal(t, tt.want.Owner, validatorFromShare(tt.share).Owner)
			require.Equal(t, tt.want.Committee, validatorFromShare(tt.share).Committee)
			require.Equal(t, tt.want.Index, validatorFromShare(tt.share).Index)
			require.Equal(t, tt.want.Status, validatorFromShare(tt.share).Status)
		})
	}
}
