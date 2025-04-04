package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/api"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// mockShare creates a SSVShare with the specified operator IDs.
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

// mockShareWithOwner creates a SSVShare with the specified owner address.
func mockShareWithOwner(owner string) *types.SSVShare {
	share := mockShare(1, 2, 3)
	owner = strings.TrimPrefix(owner, "0x")
	ownerBytes := common.FromHex(owner)

	if len(ownerBytes) < len(share.OwnerAddress) {
		paddedBytes := make([]byte, len(share.OwnerAddress))
		copy(paddedBytes, ownerBytes)
		ownerBytes = paddedBytes
	} else if len(ownerBytes) > len(share.OwnerAddress) {
		ownerBytes = ownerBytes[:len(share.OwnerAddress)]
	}

	for i := range share.OwnerAddress {
		share.OwnerAddress[i] = 0
	}
	copy(share.OwnerAddress[:], ownerBytes)
	return share
}

// mockShareWithIndex creates a SSVShare with the specified validator index.
func mockShareWithIndex(index uint64) *types.SSVShare {
	share := mockShare(1, 2, 3)
	share.ValidatorIndex = phase0.ValidatorIndex(index)
	return share
}

// mockFullShare creates a complete SSVShare with all fields populated.
func mockFullShare() *types.SSVShare {
	share := mockShare(1, 2, 3)
	pubKeyBytes := common.FromHex("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	ownerBytes := common.FromHex("0xabcdef1234567890abcdef1234567890abcdef12")

	copy(share.ValidatorPubKey[:], pubKeyBytes)
	copy(share.OwnerAddress[:], ownerBytes)

	share.ValidatorIndex = phase0.ValidatorIndex(123)
	share.Status = 1 // ValidatorStatePendingInitialized
	share.ActivationEpoch = phase0.Epoch(1000)
	share.Graffiti = []byte("test graffiti")
	share.Liquidated = true

	return share
}

// TestByClusters tests the byClusters filter function.
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

// TestByPubKeys tests the byPubKeys filter function.
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

// TestByOperators tests the byOperators filter function.
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

// TestByOwners tests the byOwners filter function.
func TestByOwners(t *testing.T) {
	t.Parallel()

	owner1 := "abcdef1234567890abcdef1234567890abcdef12"
	owner1bytes := common.FromHex(owner1)
	owner2 := "1234567890abcdef1234567890abcdef12345678"
	owner2bytes := common.FromHex(owner2)

	share1 := mockShareWithOwner(owner1)

	tests := []struct {
		name   string
		owners []api.Hex
		share  *types.SSVShare
		want   bool
	}{
		{
			name:   "matching owner",
			owners: []api.Hex{owner1bytes},
			share:  share1,
			want:   true,
		},
		{
			name:   "non-matching owner",
			owners: []api.Hex{owner2bytes},
			share:  share1,
			want:   false,
		},
		{
			name: "multiple owners with one match",
			owners: []api.Hex{
				owner2bytes,
				owner1bytes,
			},
			share: share1,
			want:  true,
		},
	}

	for _, tt := range tests {

		t.Run(strings.ToLower(tt.name), func(t *testing.T) {
			t.Parallel()

			validate := byOwners(tt.owners)
			got := validate(tt.share)

			require.Equal(t, tt.want, got, "byOwners() returned %v; want %v", got, tt.want)
		})
	}
}

// TestByIndices tests the byIndices filter function.
func TestByIndices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		indices []uint64
		share   *types.SSVShare
		want    bool
	}{
		{
			name:    "matching index",
			indices: []uint64{123},
			share:   mockShareWithIndex(123),
			want:    true,
		},
		{
			name:    "non-matching index",
			indices: []uint64{456},
			share:   mockShareWithIndex(123),
			want:    false,
		},
		{
			name:    "multiple indices with one match",
			indices: []uint64{456, 123, 789},
			share:   mockShareWithIndex(123),
			want:    true,
		},
	}

	for _, tt := range tests {

		t.Run(strings.ToLower(tt.name), func(t *testing.T) {
			t.Parallel()

			validate := byIndices(tt.indices)
			got := validate(tt.share)

			require.Equal(t, tt.want, got, "byIndices() returned %v; want %v", got, tt.want)
		})
	}
}

// TestValidatorFromShare tests the validatorFromShare function.
func TestValidatorFromShare(t *testing.T) {
	t.Parallel()

	t.Run("basic share", func(t *testing.T) {
		t.Parallel()

		share := mockShare(1, 2, 3)
		pubKeyBytes := common.FromHex("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
		ownerBytes := common.FromHex("0xabcdef1234567890abcdef1234567890abcdef12")

		copy(share.ValidatorPubKey[:], pubKeyBytes)
		copy(share.OwnerAddress[:], ownerBytes)

		share.Graffiti = []byte("test graffiti")
		share.Liquidated = true

		v := validatorFromShare(share)

		require.Equal(t, api.Hex(share.ValidatorPubKey[:]), v.PubKey)
		require.Equal(t, api.Hex(share.OwnerAddress[:]), v.Owner)
		require.Equal(t, []spectypes.OperatorID{1, 2, 3}, v.Committee)
		require.Equal(t, "test graffiti", v.Graffiti)
		require.Equal(t, true, v.Liquidated)
		require.Equal(t, phase0.ValidatorIndex(0), v.Index)
		require.Equal(t, "", v.Status)
		require.Equal(t, phase0.Epoch(0), v.ActivationEpoch)
	})

	t.Run("share with beacon metadata", func(t *testing.T) {
		t.Parallel()

		share := mockFullShare()
		v := validatorFromShare(share)

		require.Equal(t, api.Hex(share.ValidatorPubKey[:]), v.PubKey)
		require.Equal(t, api.Hex(share.OwnerAddress[:]), v.Owner)
		require.Equal(t, []spectypes.OperatorID{1, 2, 3}, v.Committee)
		require.Equal(t, "test graffiti", v.Graffiti)
		require.Equal(t, true, v.Liquidated)
		require.Equal(t, phase0.ValidatorIndex(123), v.Index)
		require.Equal(t, v1.ValidatorStatePendingInitialized.String(), v.Status)
		require.Equal(t, phase0.Epoch(1000), v.ActivationEpoch)
	})
}

// TestRequestClustersBind tests the Bind method of requestClusters.
func TestRequestClustersBind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantError bool
		expected  requestClusters
	}{
		{
			name:      "empty string",
			input:     "",
			wantError: false,
			expected:  nil,
		},
		{
			name:      "single cluster",
			input:     "1,2,3",
			wantError: false,
			expected:  requestClusters{{1, 2, 3}},
		},
		{
			name:      "multiple clusters",
			input:     "1,2,3 4,5,6 7,8,9",
			wantError: false,
			expected:  requestClusters{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
		},
		{
			name:      "invalid number",
			input:     "1,2,abc",
			wantError: true,
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var clusters requestClusters
			err := clusters.Bind(tt.input)

			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, clusters)
			}
		})
	}
}

// mockShares is a simplified implementation of storage.Shares for testing.
type mockShares struct {
	shares []*types.SSVShare
}

// newMockShares creates a new mockShares with the given shares.
func newMockShares(shares []*types.SSVShare) *mockShares {
	return &mockShares{shares: shares}
}

// List returns shares that match all the given filters.
func (m *mockShares) List(_ basedb.Reader, filters ...storage.SharesFilter) []*types.SSVShare {
	if len(filters) == 0 {
		return m.shares
	}

	var result []*types.SSVShare
SharesLoop:
	for _, share := range m.shares {
		for _, filter := range filters {
			if !filter(share) {
				continue SharesLoop
			}
		}
		result = append(result, share)
	}

	return result
}

func (m *mockShares) Get(_ basedb.Reader, _ []byte) (*types.SSVShare, bool) {
	return nil, false
}

func (m *mockShares) Range(_ basedb.Reader, _ func(*types.SSVShare) bool) {
	// no-op
}

func (m *mockShares) Save(_ basedb.ReadWriter, _ ...*types.SSVShare) error {
	return nil
}

func (m *mockShares) Delete(_ basedb.ReadWriter, _ []byte) error {
	return nil
}

func (m *mockShares) Drop() error {
	return nil
}

func (m *mockShares) UpdateValidatorsMetadata(_ map[spectypes.ValidatorPK]*beaconprotocol.ValidatorMetadata) error {
	return nil
}

// TestValidatorsList tests the List method of the Validators handler.
func TestValidatorsList(t *testing.T) {
	t.Parallel()

	share := mockFullShare()
	ownerAddr := "abcdef1234567890abcdef1234567890abcdef12"
	ownerBytes := common.FromHex(ownerAddr)

	for i := range share.OwnerAddress {
		share.OwnerAddress[i] = 0
	}
	copy(share.OwnerAddress[:], ownerBytes)

	testCases := []struct {
		name           string
		queryParams    url.Values
		mockShares     []*types.SSVShare
		expectedShares []*types.SSVShare
		wantStatus     int
		wantError      bool
	}{
		{
			name:           "no filters",
			queryParams:    url.Values{},
			mockShares:     []*types.SSVShare{share},
			expectedShares: []*types.SSVShare{share},
			wantStatus:     http.StatusOK,
		},
		{
			name: "with owner filter",
			queryParams: url.Values{
				"owners": []string{"0x" + ownerAddr},
			},
			mockShares:     []*types.SSVShare{share},
			expectedShares: []*types.SSVShare{share},
			wantStatus:     http.StatusOK,
		},
		{
			name: "with operators filter",
			queryParams: url.Values{
				"operators": []string{"1,2,3"},
			},
			mockShares:     []*types.SSVShare{share},
			expectedShares: []*types.SSVShare{share},
			wantStatus:     http.StatusOK,
		},
		{
			name: "with multiple filters",
			queryParams: url.Values{
				"owners":    []string{"0x" + ownerAddr},
				"operators": []string{"1,2,3"},
				"clusters":  []string{"1,2,3"},
				"pubkeys":   []string{"0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"},
				"indices":   []string{"123"},
			},
			mockShares:     []*types.SSVShare{share},
			expectedShares: []*types.SSVShare{share},
			wantStatus:     http.StatusOK,
		},
		{
			name: "invalid cluster param",
			queryParams: url.Values{
				"clusters": []string{"1,2,abc"},
			},
			mockShares: []*types.SSVShare{},
			wantError:  true,
		},
		{
			name: "with subclusters filter",
			queryParams: url.Values{
				"subclusters": []string{"1,2"},
			},
			mockShares:     []*types.SSVShare{share},
			expectedShares: []*types.SSVShare{share},
			wantStatus:     http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest("GET", "/validators?"+tc.queryParams.Encode(), nil)

			require.NoError(t, err)

			rr := httptest.NewRecorder()
			handler := &Validators{
				Shares: newMockShares(tc.mockShares),
			}

			err = handler.List(rr, req)

			if tc.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, rr.Code)

			var response struct {
				Data []*validatorJSON `json:"data"`
			}

			err = json.Unmarshal(rr.Body.Bytes(), &response)

			require.NoError(t, err)
			require.Len(t, response.Data, len(tc.expectedShares), "expected %d validators, got %d", len(tc.expectedShares), len(response.Data))

			for i, expectedShare := range tc.expectedShares {
				expected := validatorFromShare(expectedShare)
				actual := response.Data[i]

				assert.Equal(t, expected.PubKey, actual.PubKey)
				assert.Equal(t, expected.Owner, actual.Owner)
				assert.ElementsMatch(t, expected.Committee, actual.Committee)
				assert.Equal(t, expected.Graffiti, actual.Graffiti)
				assert.Equal(t, expected.Liquidated, actual.Liquidated)

				if expectedShare.HasBeaconMetadata() {
					assert.Equal(t, expected.Index, actual.Index)
					assert.Equal(t, expected.Status, actual.Status)
					assert.Equal(t, expected.ActivationEpoch, actual.ActivationEpoch)
				}
			}
		})
	}
}
