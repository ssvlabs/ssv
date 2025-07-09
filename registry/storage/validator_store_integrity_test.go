package storage

import (
	"context"
	"math"
	"math/rand"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

// TestValidatorStore_Integrity performs comprehensive integrity checks on the ValidatorStore
// to ensure all methods return consistent results after various operations.
func TestValidatorStore_Integrity(t *testing.T) {
	ctx := context.Background()

	// Create test shares with various configurations
	shares := createIntegrityTestShares()

	tests := []struct {
		name       string
		operations func(t *testing.T, store ValidatorStore) []*types.SSVShare
		validate   func(t *testing.T, store ValidatorStore, shares []*types.SSVShare)
	}{
		{
			name: "after_initial_load",
			operations: func(t *testing.T, store ValidatorStore) []*types.SSVShare {
				// Store already initialized with shares
				return shares
			},
			validate: func(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
				requireValidatorStoreIntegrity(t, store, shares)
			},
		},
		{
			name: "after_adds",
			operations: func(t *testing.T, store ValidatorStore) []*types.SSVShare {
				// Add more shares
				newShares := createAdditionalShares(10, 4)
				for _, share := range newShares {
					err := store.OnShareAdded(ctx, share, UpdateOptions{})
					require.NoError(t, err)
				}
				return append(shares, newShares...)
			},
			validate: func(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
				requireValidatorStoreIntegrity(t, store, shares)
			},
		},
		{
			name: "after_updates",
			operations: func(t *testing.T, store ValidatorStore) []*types.SSVShare {
				// Update some shares
				updatedShares := make([]*types.SSVShare, len(shares))
				copy(updatedShares, shares)

				for i := 0; i < len(updatedShares)/2; i++ {
					updated := updatedShares[i].Copy()
					updated.Liquidated = i%2 == 0
					err := store.OnShareUpdated(ctx, updated, UpdateOptions{})
					require.NoError(t, err)
					updatedShares[i] = updated
				}
				return updatedShares
			},
			validate: func(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
				requireValidatorStoreIntegrity(t, store, shares)
			},
		},
		{
			name: "after_cluster_operations",
			operations: func(t *testing.T, store ValidatorStore) []*types.SSVShare {
				// Liquidate and reactivate clusters
				owner := shares[0].OwnerAddress
				operatorIDs := shares[0].OperatorIDs()

				err := store.OnClusterLiquidated(ctx, owner, operatorIDs, UpdateOptions{})
				require.NoError(t, err)

				// Update shares to reflect liquidation
				updatedShares := make([]*types.SSVShare, len(shares))
				copy(updatedShares, shares)

				for i, share := range updatedShares {
					if share.OwnerAddress == owner && sameOperators(share.OperatorIDs(), operatorIDs) {
						updated := share.Copy()
						updated.Liquidated = true
						updatedShares[i] = updated
					}
				}

				err = store.OnClusterReactivated(ctx, owner, operatorIDs, UpdateOptions{})
				require.NoError(t, err)

				// Update shares to reflect reactivation
				for i, share := range updatedShares {
					if share.OwnerAddress == owner && sameOperators(share.OperatorIDs(), operatorIDs) {
						updated := share.Copy()
						updated.Liquidated = false
						updatedShares[i] = updated
					}
				}

				return updatedShares
			},
			validate: func(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
				requireValidatorStoreIntegrity(t, store, shares)
			},
		},
		{
			name: "after_removals",
			operations: func(t *testing.T, store ValidatorStore) []*types.SSVShare {
				// Remove some shares
				toRemove := len(shares) / 4
				for i := 0; i < toRemove; i++ {
					err := store.OnShareRemoved(ctx, shares[i].ValidatorPubKey, UpdateOptions{})
					require.NoError(t, err)
				}
				return shares[toRemove:]
			},
			validate: func(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
				requireValidatorStoreIntegrity(t, store, shares)
			},
		},
		{
			name: "after_metadata_updates",
			operations: func(t *testing.T, store ValidatorStore) []*types.SSVShare {
				// Update metadata for some validators
				metadata := make(beacon.ValidatorMetadataMap)
				for i := 0; i < len(shares)/3; i++ {
					metadata[shares[i].ValidatorPubKey] = &beacon.ValidatorMetadata{
						ActivationEpoch: phase0.Epoch(i * 10),
						ExitEpoch:       phase0.Epoch(^uint64(0) >> 1),
						Index:           shares[i].ValidatorIndex,
						Status:          eth2apiv1.ValidatorStateActiveExiting,
					}
				}

				changed, err := store.UpdateValidatorsMetadata(ctx, metadata)
				require.NoError(t, err)

				// Update shares to reflect metadata changes
				updatedShares := make([]*types.SSVShare, len(shares))
				copy(updatedShares, shares)

				for pk, md := range changed {
					for i, share := range updatedShares {
						if share.ValidatorPubKey == pk {
							updated := share.Copy()
							updated.SetBeaconMetadata(md)
							updatedShares[i] = updated
						}
					}
				}

				return updatedShares
			},
			validate: func(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
				requireValidatorStoreIntegrity(t, store, shares)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh store for each test
			store, _, _ := createTestStore(t, shares...)

			updatedShares := tt.operations(t, store)
			tt.validate(t, store, updatedShares)
		})
	}
}

// requireValidatorStoreIntegrity checks that every function of the ValidatorStore returns the expected results
// by reconstructing the expected state from the given shares and comparing it to the actual state of the store.
func requireValidatorStoreIntegrity(t *testing.T, store ValidatorStore, shares []*types.SSVShare) {
	// Check that there are no false positives.
	const nonExistingIndex = phase0.ValidatorIndex(math.MaxUint64 - 1)
	const nonExistingOperatorID = spectypes.OperatorID(math.MaxUint64 - 1)
	var nonExistingCommitteeID spectypes.CommitteeID
	n, err := rand.Read(nonExistingCommitteeID[:])
	require.NoError(t, err)
	require.Equal(t, len(nonExistingCommitteeID), n)

	// Check non-existing items return nil/empty
	validator, exists := store.GetValidator(ValidatorIndex(nonExistingIndex))
	require.False(t, exists)
	require.Nil(t, validator)

	operatorValidators := store.GetOperatorValidators(nonExistingOperatorID)
	require.Empty(t, operatorValidators)

	committee, exists := store.GetCommittee(nonExistingCommitteeID)
	require.False(t, exists)
	require.Nil(t, committee)

	// Filter out non-metadata shares for certain checks
	sharesWithMetadata := make([]*types.SSVShare, 0)
	for _, share := range shares {
		if share.HasBeaconMetadata() {
			sharesWithMetadata = append(sharesWithMetadata, share)
		}
	}

	// Check GetValidator by pubkey
	for _, share := range shares {
		v, exists := store.GetValidator(ValidatorPubKey(share.ValidatorPubKey))
		require.True(t, exists, "validator %x not found by pubkey", share.ValidatorPubKey)
		requireEqualShare(t, share, &v.Share)
	}

	// Check GetValidator by index (only for shares with metadata)
	for _, share := range sharesWithMetadata {
		v, exists := store.GetValidator(ValidatorIndex(share.ValidatorIndex))
		require.True(t, exists, "validator %d not found by index", share.ValidatorIndex)
		requireEqualShare(t, share, &v.Share)
	}

	// Check GetAllValidators
	allValidators := store.GetAllValidators()
	require.Len(t, allValidators, len(shares))

	// Verify all shares are present
	foundShares := make(map[spectypes.ValidatorPK]bool)
	for _, v := range allValidators {
		foundShares[v.Share.ValidatorPubKey] = true
	}
	for _, share := range shares {
		require.True(t, foundShares[share.ValidatorPubKey], "share %x not in GetAllValidators", share.ValidatorPubKey)
	}

	// Reconstruct hierarchy to check integrity of operators and committees.
	byOperator := make(map[spectypes.OperatorID][]*types.SSVShare)
	byCommittee := make(map[spectypes.CommitteeID][]*types.SSVShare)
	committeeOperators := make(map[spectypes.CommitteeID]map[spectypes.OperatorID]struct{})
	operatorCommittees := make(map[spectypes.OperatorID]map[spectypes.CommitteeID]struct{})

	for _, share := range shares {
		id := share.CommitteeID()
		byCommittee[id] = append(byCommittee[id], share)
		for _, operator := range share.Committee {
			byOperator[operator.Signer] = append(byOperator[operator.Signer], share)

			if committeeOperators[id] == nil {
				committeeOperators[id] = make(map[spectypes.OperatorID]struct{})
			}
			committeeOperators[id][operator.Signer] = struct{}{}

			if operatorCommittees[operator.Signer] == nil {
				operatorCommittees[operator.Signer] = make(map[spectypes.CommitteeID]struct{})
			}
			operatorCommittees[operator.Signer][id] = struct{}{}
		}
	}

	// Check GetOperatorValidators
	for operatorID, expectedShares := range byOperator {
		opValidators := store.GetOperatorValidators(operatorID)
		require.Len(t, opValidators, len(expectedShares), "operator %d validator count mismatch", operatorID)

		// Verify all expected shares are present
		foundValidators := make(map[spectypes.ValidatorPK]bool)
		for _, v := range opValidators {
			foundValidators[v.Share.ValidatorPubKey] = true
		}
		for _, share := range expectedShares {
			require.True(t, foundValidators[share.ValidatorPubKey], "validator %x not found for operator %d", share.ValidatorPubKey, operatorID)
		}
	}

	// Check committees
	committees := store.GetCommittees()
	require.Len(t, committees, len(byCommittee))

	for committeeID, expectedValidators := range byCommittee {
		committee, exists := store.GetCommittee(committeeID)
		require.True(t, exists, "committee %x not found", committeeID)
		require.Len(t, committee.Validators, len(expectedValidators))

		// Verify all validators in committee
		foundValidators := make(map[spectypes.ValidatorPK]bool)
		for _, v := range committee.Validators {
			foundValidators[v.Share.ValidatorPubKey] = true
		}
		for _, share := range expectedValidators {
			require.True(t, foundValidators[share.ValidatorPubKey], "validator %x not in committee", share.ValidatorPubKey)
		}

		// Verify operators
		expectedOperators := make(map[spectypes.OperatorID]struct{})
		for _, share := range expectedValidators {
			for _, member := range share.Committee {
				expectedOperators[member.Signer] = struct{}{}
			}
		}
		require.Len(t, committee.Operators, len(expectedOperators))
		for _, op := range committee.Operators {
			_, exists := expectedOperators[op]
			require.True(t, exists, "operator %d not expected in committee", op)
		}
	}

	// Check GetOperatorCommittees
	for operatorID, expectedCommittees := range operatorCommittees {
		opCommittees := store.GetOperatorCommittees(operatorID)
		require.Len(t, opCommittees, len(expectedCommittees), "operator %d has %d committees, but %d in store", operatorID, len(expectedCommittees), len(opCommittees))

		foundCommittees := make(map[spectypes.CommitteeID]bool)
		for _, committee := range opCommittees {
			foundCommittees[committee.ID] = true

			// Verify committee has the operator
			hasOperator := false
			for _, op := range committee.Operators {
				if op == operatorID {
					hasOperator = true
					break
				}
			}
			require.True(t, hasOperator, "committee %x doesn't have operator %d", committee.ID, operatorID)
		}

		for committeeID := range expectedCommittees {
			require.True(t, foundCommittees[committeeID], "committee %x not found for operator %d", committeeID, operatorID)
		}
	}

	// Check GetParticipatingValidators
	currentEpoch := testNetworkConfig.EstimatedCurrentEpoch()
	var expectedParticipating []*types.SSVShare
	for _, share := range shares {
		if share.IsParticipating(testNetworkConfig, currentEpoch) &&
			share.ActivationEpoch <= currentEpoch &&
			share.Status != eth2apiv1.ValidatorStatePendingQueued {
			expectedParticipating = append(expectedParticipating, share)
		}
	}

	participatingValidators := store.GetParticipatingValidators(currentEpoch, ParticipationOptions{})
	require.Len(t, participatingValidators, len(expectedParticipating))

	foundParticipating := make(map[spectypes.ValidatorPK]bool)
	for _, v := range participatingValidators {
		foundParticipating[v.Share.ValidatorPubKey] = true
	}
	for _, share := range expectedParticipating {
		require.True(t, foundParticipating[share.ValidatorPubKey], "participating validator %x not found", share.ValidatorPubKey)
	}

	// Check participating committees (derived from GetCommittees and participation status)
	participatingCommittees := make(map[spectypes.CommitteeID]bool)
	for _, committee := range committees {
		hasParticipating := false
		for _, validator := range committee.Validators {
			if validator.ParticipationStatus.IsParticipating {
				hasParticipating = true
				break
			}
		}
		if hasParticipating {
			participatingCommittees[committee.ID] = true
		}
	}

	// Verify against expected
	expectedParticipatingCommittees := make(map[spectypes.CommitteeID]struct{})
	for _, share := range expectedParticipating {
		expectedParticipatingCommittees[share.CommitteeID()] = struct{}{}
	}
	require.Len(t, participatingCommittees, len(expectedParticipatingCommittees))
}

// Helper functions for integrity tests
func requireEqualShare(t *testing.T, expected *types.SSVShare, actual *types.SSVShare) {
	require.Equal(t, expected.ValidatorPubKey, actual.ValidatorPubKey, "validator pubkey mismatch")
	require.Equal(t, expected.ValidatorIndex, actual.ValidatorIndex, "validator index mismatch")
	require.Equal(t, expected.Status, actual.Status, "status mismatch")
	require.Equal(t, expected.ActivationEpoch, actual.ActivationEpoch, "activation epoch mismatch")
	require.Equal(t, expected.ExitEpoch, actual.ExitEpoch, "exit epoch mismatch")
	require.Equal(t, expected.OwnerAddress, actual.OwnerAddress, "owner address mismatch")
	require.Equal(t, expected.Liquidated, actual.Liquidated, "liquidated status mismatch")
	require.Equal(t, expected.FeeRecipientAddress, actual.FeeRecipientAddress, "fee recipient mismatch")
	require.Equal(t, len(expected.Committee), len(actual.Committee), "committee size mismatch")

	// Check committee members - build maps for easier comparison
	expectedMembers := make(map[spectypes.OperatorID][]byte)
	for _, member := range expected.Committee {
		expectedMembers[member.Signer] = member.SharePubKey
	}

	actualMembers := make(map[spectypes.OperatorID][]byte)
	for _, member := range actual.Committee {
		actualMembers[member.Signer] = member.SharePubKey
	}

	// Compare the maps
	require.Equal(t, len(expectedMembers), len(actualMembers), "committee member count mismatch")
	for operatorID, expectedPubKey := range expectedMembers {
		actualPubKey, exists := actualMembers[operatorID]
		require.True(t, exists, "missing committee member %d", operatorID)
		require.Equal(t, expectedPubKey, actualPubKey, "share pubkey mismatch for operator %d", operatorID)
	}
}

func sameOperators(a, b []spectypes.OperatorID) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[spectypes.OperatorID]bool)
	for _, id := range a {
		aMap[id] = true
	}
	for _, id := range b {
		if !aMap[id] {
			return false
		}
	}
	return true
}

func createIntegrityTestShares() []*types.SSVShare {
	return []*types.SSVShare{
		{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
				SharePubKey:     spectypes.ShareValidatorPK{4, 5, 6},
				Committee: []*spectypes.ShareMember{
					{Signer: 1, SharePubKey: []byte{1, 1, 1}},
					{Signer: 2, SharePubKey: []byte{2, 2, 2}},
					{Signer: 3, SharePubKey: []byte{3, 3, 3}},
					{Signer: 4, SharePubKey: []byte{4, 4, 4}},
				},
				FeeRecipientAddress: [20]byte{10, 20, 30},
			},
			Status:          eth2apiv1.ValidatorStateActiveOngoing,
			ActivationEpoch: 0,
			ExitEpoch:       phase0.Epoch(^uint64(0) >> 1),
			OwnerAddress:    common.HexToAddress("0x12345"),
		},
		{
			Share: spectypes.Share{
				ValidatorIndex:  2,
				ValidatorPubKey: spectypes.ValidatorPK{7, 8, 9},
				SharePubKey:     spectypes.ShareValidatorPK{10, 11, 12},
				Committee: []*spectypes.ShareMember{
					{Signer: 2, SharePubKey: []byte{2, 2, 2}},
					{Signer: 3, SharePubKey: []byte{3, 3, 3}},
					{Signer: 4, SharePubKey: []byte{4, 4, 4}},
					{Signer: 5, SharePubKey: []byte{5, 5, 5}},
				},
				FeeRecipientAddress: [20]byte{40, 50, 60},
			},
			Status:          eth2apiv1.ValidatorStatePendingQueued,
			ActivationEpoch: 200,
			ExitEpoch:       phase0.Epoch(^uint64(0) >> 1),
			OwnerAddress:    common.HexToAddress("0x67890"),
		},
		{
			Share: spectypes.Share{
				ValidatorIndex:  3,
				ValidatorPubKey: spectypes.ValidatorPK{13, 14, 15},
				SharePubKey:     spectypes.ShareValidatorPK{16, 17, 18},
				Committee: []*spectypes.ShareMember{
					{Signer: 1, SharePubKey: []byte{1, 1, 1}},
					{Signer: 2, SharePubKey: []byte{2, 2, 2}},
				},
				FeeRecipientAddress: [20]byte{70, 80, 90},
			},
			Status:          eth2apiv1.ValidatorStateActiveExiting,
			ActivationEpoch: 100,
			ExitEpoch:       500,
			OwnerAddress:    common.HexToAddress("0xabcde"),
		},
	}
}

func createAdditionalShares(count int, committeeSize int) []*types.SSVShare {
	shares := make([]*types.SSVShare, count)
	for i := range shares {
		committee := make([]*spectypes.ShareMember, committeeSize)
		for j := range committee {
			committee[j] = &spectypes.ShareMember{
				Signer:      spectypes.OperatorID(j + 1),
				SharePubKey: []byte{byte(100 + i), byte(j), byte(i + j)},
			}
		}

		shares[i] = &types.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:      phase0.ValidatorIndex(100 + i),
				ValidatorPubKey:     spectypes.ValidatorPK{byte(100 + i), byte(i), byte(i + 1)},
				SharePubKey:         spectypes.ShareValidatorPK{byte(i + 2), byte(i + 3), byte(i + 4)},
				Committee:           committee,
				FeeRecipientAddress: [20]byte{byte(i)},
			},
			Status:          eth2apiv1.ValidatorStateActiveOngoing,
			ActivationEpoch: 0,
			ExitEpoch:       phase0.Epoch(^uint64(0) >> 1),
			OwnerAddress:    common.HexToAddress("0x12345"),
		}
	}
	return shares
}
