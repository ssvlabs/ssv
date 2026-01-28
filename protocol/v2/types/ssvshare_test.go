package types

import (
	"encoding/hex"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

func TestSSVShare_BelongsToOperator(t *testing.T) {
	metadata := &SSVShare{
		Share: spectypes.Share{
			Committee: []*spectypes.ShareMember{{Signer: 1}},
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
			Name:          "SSVShare == nil",
			ShareMetadata: nil,
			Result:        false,
		},
		{
			Name:          "SSVShare.Status == ValidatorStateUnknown",
			ShareMetadata: &SSVShare{},
			Result:        false,
		},
		{
			Name: "SSVShare.Status != ValidatorStateUnknown",
			ShareMetadata: &SSVShare{
				Status: eth2apiv1.ValidatorStatePendingInitialized,
			},
			Result: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Result, tc.ShareMetadata.HasBeaconMetadata())
		})
	}
}

func TestValidCommitteeSize(t *testing.T) {
	tt := []struct {
		name  string
		valid bool
		sizes []uint64
	}{
		{
			name:  "valid",
			valid: true,
			sizes: []uint64{4, 7, 10, 13},
		},
		{
			name:  "invalid",
			valid: false,
			sizes: []uint64{0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 14, 15, 16, 17, 18, 19},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range tc.sizes {
				require.Equal(t, tc.valid, ValidCommitteeSize(size))
			}
		})
	}
}

func TestSMRQuorumCalculations(t *testing.T) {
	t.Run("ComputeSMRF", func(t *testing.T) {
		tt := []struct {
			committeeSize uint64
			expectedF     uint64
		}{
			{committeeSize: 0, expectedF: 0},
			{committeeSize: 1, expectedF: 0},
			{committeeSize: 2, expectedF: 0},
			{committeeSize: 3, expectedF: 0},
			{committeeSize: 4, expectedF: 1},
			{committeeSize: 7, expectedF: 1},
			{committeeSize: 8, expectedF: 1},
			{committeeSize: 9, expectedF: 2},
			{committeeSize: 10, expectedF: 2},
			{committeeSize: 13, expectedF: 2},
			{committeeSize: 14, expectedF: 3},
		}

		for _, tc := range tt {
			require.Equal(t, tc.expectedF, ComputeSMRF(tc.committeeSize))
		}
	})

	t.Run("ComputeSMRQuorum", func(t *testing.T) {
		tt := []struct {
			committeeSize uint64
			expectedQ     uint64
		}{
			{committeeSize: 0, expectedQ: 0},
			{committeeSize: 3, expectedQ: 0},
			{committeeSize: 4, expectedQ: 3},
			{committeeSize: 7, expectedQ: 3},
			{committeeSize: 10, expectedQ: 7},
			{committeeSize: 13, expectedQ: 7},
			{committeeSize: 14, expectedQ: 11},
		}

		for _, tc := range tt {
			require.Equal(t, tc.expectedQ, ComputeSMRQuorum(tc.committeeSize))
		}
	})

	t.Run("ValidSMRCommitteeSize", func(t *testing.T) {
		tt := []struct {
			committeeSize uint64
			valid         bool
		}{
			{committeeSize: 0, valid: false},
			{committeeSize: 1, valid: false},
			{committeeSize: 2, valid: false},
			{committeeSize: 3, valid: false},
			{committeeSize: 4, valid: true},
			{committeeSize: 5, valid: true},
			{committeeSize: 7, valid: true},
			{committeeSize: 8, valid: true},
			{committeeSize: 9, valid: true},
			{committeeSize: 10, valid: true},
			{committeeSize: 13, valid: true},
			{committeeSize: 14, valid: true},
		}

		for _, tc := range tt {
			require.Equal(t, tc.valid, ValidSMRCommitteeSize(tc.committeeSize))
		}
	})

	t.Run("CommitteeMemberWrapper", func(t *testing.T) {
		tt := []struct {
			committeeSize uint64
			expectedQ     uint64
		}{
			{committeeSize: 4, expectedQ: 3},
			{committeeSize: 7, expectedQ: 3},
			{committeeSize: 10, expectedQ: 7},
			{committeeSize: 13, expectedQ: 7},
		}

		for _, tc := range tt {
			cm := &spectypes.CommitteeMember{
				Committee: make([]*spectypes.Operator, int(tc.committeeSize)),
			}
			wrapped := SMRCommitteeMember{CommitteeMember: cm}
			require.Equal(t, tc.expectedQ, wrapped.GetSMRQuorum())

			require.False(t, wrapped.HasSMRQuorum(-1))
			require.False(t, wrapped.HasSMRQuorum(int(tc.expectedQ-1)))
			require.True(t, wrapped.HasSMRQuorum(int(tc.expectedQ)))
		}

		require.Equal(t, uint64(0), SMRCommitteeMember{}.GetSMRQuorum())
		require.False(t, SMRCommitteeMember{}.HasSMRQuorum(0))
	})

	t.Run("CompareQBFTAndSMRQuorums", func(t *testing.T) {
		tt := []struct {
			committeeSize uint64
			qbftF         uint64
			qbftQ         uint64
			smrF          uint64
			smrQ          uint64
		}{
			{committeeSize: 4, qbftF: 1, qbftQ: 3, smrF: 1, smrQ: 3},
			{committeeSize: 7, qbftF: 2, qbftQ: 5, smrF: 1, smrQ: 3},
			{committeeSize: 10, qbftF: 3, qbftQ: 7, smrF: 2, smrQ: 7},
			{committeeSize: 13, qbftF: 4, qbftQ: 9, smrF: 2, smrQ: 7},
		}

		for _, tc := range tt {
			qbftQ, _ := ComputeQuorumAndPartialQuorum(tc.committeeSize)
			require.Equal(t, tc.qbftF, ComputeF(tc.committeeSize))
			require.Equal(t, tc.qbftQ, qbftQ)

			require.Equal(t, tc.smrF, ComputeSMRF(tc.committeeSize))
			require.Equal(t, tc.smrQ, ComputeSMRQuorum(tc.committeeSize))
		}
	})
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
			Name:     "No BeaconMetadata",
			Share:    &SSVShare{},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Is Attesting",
			Share: &SSVShare{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Is Attesting and Liquidated",
			Share: &SSVShare{
				Status:     eth2apiv1.ValidatorStateActiveOngoing,
				Liquidated: true,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Pending Queued with Future Activation Epoch",
			Share: &SSVShare{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: currentEpoch + 1,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Pending Queued with Current Epoch as Activation Epoch",
			Share: &SSVShare{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: currentEpoch,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Pending Queued with Past Activation Epoch",
			Share: &SSVShare{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: currentEpoch - 1,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			result := tc.Share.IsAttesting(tc.Epoch)
			require.Equal(t, tc.Expected, result)
		})
	}
}

func TestSSVShare_IsParticipating(t *testing.T) {
	currentEpoch := phase0.Epoch(100)
	tt := []struct {
		Name     string
		Share    *SSVShare
		Epoch    phase0.Epoch
		Expected bool
	}{
		{
			Name: "Liquidated Share",
			Share: &SSVShare{
				Status:     eth2apiv1.ValidatorStateActiveOngoing,
				Liquidated: true,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Non-Liquidated Share that is Attesting",
			Share: &SSVShare{
				Status:     eth2apiv1.ValidatorStateActiveOngoing,
				Liquidated: false,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Non-Liquidated Share that is not Attesting",
			Share: &SSVShare{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: currentEpoch + 1,
				Liquidated:      false,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Non-Liquidated Share with Pending Queued and Current Epoch Activation",
			Share: &SSVShare{
				Status:          eth2apiv1.ValidatorStatePendingQueued,
				ActivationEpoch: currentEpoch,
				Liquidated:      false,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			result := tc.Share.IsParticipating(networkconfig.TestNetwork.Beacon, tc.Epoch)
			require.Equal(t, tc.Expected, result)
		})
	}
}

func TestIsSyncCommitteeEligible(t *testing.T) {
	var (
		//[0-255] [256-512] [513-768]
		currentEpoch          = phase0.Epoch(600)
		epochSamePeriod       = phase0.Epoch(520)
		epochPreviousPeriod   = phase0.Epoch(256)
		epochIneligiblePeriod = phase0.Epoch(255)
	)
	tt := []struct {
		Name     string
		Share    *SSVShare
		Epoch    phase0.Epoch
		Expected bool
	}{
		{
			Name: "Attesting Share",
			Share: &SSVShare{
				Status: eth2apiv1.ValidatorStateActiveOngoing,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Exited Share Within Same Period",
			Share: &SSVShare{
				Status:    eth2apiv1.ValidatorStateExitedUnslashed,
				ExitEpoch: epochSamePeriod,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Exited Share Previous Period",
			Share: &SSVShare{
				Status:    eth2apiv1.ValidatorStateExitedUnslashed,
				ExitEpoch: epochPreviousPeriod,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Exited Share more than 1 periods ago",
			Share: &SSVShare{
				Status:    eth2apiv1.ValidatorStateExitedUnslashed,
				ExitEpoch: epochIneligiblePeriod,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Withdrawal Possible Within Same Period",
			Share: &SSVShare{
				Status:    eth2apiv1.ValidatorStateWithdrawalPossible,
				ExitEpoch: epochSamePeriod,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Slashed Within Same Period",
			Share: &SSVShare{
				Status:    eth2apiv1.ValidatorStateActiveSlashed,
				ExitEpoch: epochPreviousPeriod,
			},
			Epoch:    currentEpoch,
			Expected: true,
		},
		{
			Name: "Non-Participating Non-Exited Share",
			Share: &SSVShare{
				Status: eth2apiv1.ValidatorStatePendingInitialized,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Exited Share Within Future Period",
			Share: &SSVShare{
				Status:    eth2apiv1.ValidatorStateExitedUnslashed,
				ExitEpoch: currentEpoch * 2,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
		{
			Name: "Exited Share Within Same Period and Liquidated",
			Share: &SSVShare{
				Status:     eth2apiv1.ValidatorStateExitedUnslashed,
				ExitEpoch:  epochSamePeriod,
				Liquidated: true,
			},
			Epoch:    currentEpoch,
			Expected: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			result := tc.Share.isSyncCommitteeEligible(networkconfig.TestNetwork.Beacon, tc.Epoch)
			require.Equal(t, tc.Expected, result)
		})
	}
}
