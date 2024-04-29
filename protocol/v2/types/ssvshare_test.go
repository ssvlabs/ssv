package types

import (
	"encoding/hex"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/crypto"
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"

	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

func TestSSVShare_BelongsToOperator(t *testing.T) {
	metadata := &SSVShare{
		Share: spectypes.Share{
			OperatorID: 1,
		},
	}

	require.True(t, metadata.BelongsToOperator(1))
	require.False(t, metadata.BelongsToOperator(2))
}
func TestSSVShare_ComputeClusterIDHash(t *testing.T) {
	var (
		aliceClusterHash = "a341933234aa1e6dfd3b8d6677172bdcd0986b1e6afc2e84d321f154d9736717"
		testKeyAlice, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		testAddrAlice    = crypto.PubkeyToAddress(testKeyAlice.PublicKey)
	)

	clusterHash := ComputeClusterIDHash(testAddrAlice, []uint64{3, 2, 1, 4})
	clusterHash2 := ComputeClusterIDHash(testAddrAlice, []uint64{4, 3, 1, 2})
	// Convert the hash to a hexadecimal string
	hashString := hex.EncodeToString(clusterHash)
	hashString2 := hex.EncodeToString(clusterHash2)

	require.Equal(t, aliceClusterHash, hashString)
	require.Equal(t, aliceClusterHash, hashString2)
}

func TestSSVShare_HasBeaconMetadata(t *testing.T) {
	tt := []struct {
		Name          string
		ShareMetadata *SSVShare
		Result        bool
	}{
		{
			Name:          "Metadata == nil",
			ShareMetadata: nil,
			Result:        false,
		},
		{
			Name: "BeaconMetadata == nil",
			ShareMetadata: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: nil,
				},
			},
			Result: false,
		},
		{
			Name: "BeaconMetadata != nil",
			ShareMetadata: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{},
				},
			},
			Result: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Result, tc.ShareMetadata.HasBeaconMetadata())
		})
	}
}

func TestValidCommitteeSize(t *testing.T) {
	tt := []struct {
		name  string
		valid bool
		sizes []int
	}{
		{
			name:  "valid",
			valid: true,
			sizes: []int{4, 7, 10, 13},
		},
		{
			name:  "invalid",
			valid: false,
			sizes: []int{0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 14, 15, 16, 17, 18, 19, -1, -4, -7},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range tc.sizes {
				require.Equal(t, tc.valid, ValidCommitteeSize(size))
			}
		})
	}
}

func TestSSVShare_IsAttesting(t *testing.T) {
	currentEpoch := phase0.Epoch(100) // Example current epoch for testing
	tt := []struct {
		Name     string
		Share    *SSVShare
		Epoch    phase0.Epoch
		Expected bool
	}{
		{
			Name: "No BeaconMetadata",
			Share: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: nil,
				},
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Is Attesting",
			Share: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Status: eth2apiv1.ValidatorStateActiveOngoing,
					},
				},
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Pending Queued with Future Activation Epoch",
			Share: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStatePendingQueued,
						ActivationEpoch: currentEpoch + 1,
					},
				},
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Pending Queued with Current Epoch as Activation Epoch",
			Share: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStatePendingQueued,
						ActivationEpoch: currentEpoch,
					},
				},
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Pending Queued with Past Activation Epoch",
			Share: &SSVShare{
				Metadata: Metadata{
					BeaconMetadata: &beaconprotocol.ValidatorMetadata{
						Status:          eth2apiv1.ValidatorStatePendingQueued,
						ActivationEpoch: currentEpoch - 1,
					},
				},
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			result := tc.Share.IsAttesting(tc.Epoch)
			require.Equal(t, tc.Expected, result)
		})
	}
}
