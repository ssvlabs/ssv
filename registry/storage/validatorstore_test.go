package storage

import (
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"math/rand"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aquasecurity/table"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

var share1 = &ssvtypes.SSVShare{
	Share: spectypes.Share{
		ValidatorIndex:  phase0.ValidatorIndex(1),
		ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
		SharePubKey:     spectypes.ShareValidatorPK{4, 5, 6},
		Committee:       []*spectypes.ShareMember{{Signer: 1}},
		Graffiti:        []byte("example"),
	},
	ActivationEpoch: 100,
	ExitEpoch:       goclient.FarFutureEpoch,
	Status:          eth2apiv1.ValidatorStatePendingQueued,
	OwnerAddress:    common.HexToAddress("0x12345"),
	Liquidated:      false,
}

var share2 = &ssvtypes.SSVShare{
	Share: spectypes.Share{
		ValidatorIndex:  phase0.ValidatorIndex(2),
		ValidatorPubKey: spectypes.ValidatorPK{7, 8, 9},
		SharePubKey:     spectypes.ShareValidatorPK{10, 11, 12},
		Committee:       []*spectypes.ShareMember{{Signer: 2}},
		Graffiti:        []byte("test"),
	},
	ActivationEpoch: 200,
	ExitEpoch:       goclient.FarFutureEpoch,
	Status:          eth2apiv1.ValidatorStatePendingQueued,
	OwnerAddress:    common.HexToAddress("0x67890"),
	Liquidated:      false,
}

var updatedShare2 = &ssvtypes.SSVShare{
	Share: spectypes.Share{
		ValidatorIndex:  phase0.ValidatorIndex(2),
		ValidatorPubKey: spectypes.ValidatorPK{7, 8, 9},
		SharePubKey:     spectypes.ShareValidatorPK{10, 11, 12},
		Committee:       []*spectypes.ShareMember{{Signer: 2}},
		Graffiti:        []byte("test"),
	},
	ActivationEpoch: 200,
	ExitEpoch:       goclient.FarFutureEpoch,
	Status:          eth2apiv1.ValidatorStatePendingQueued,
	OwnerAddress:    common.HexToAddress("0x67890"),
	Liquidated:      true,
}

var networkConfig = networkconfig.TestNetwork

func TestValidatorStore(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	selfStore := store.WithOperatorID(func() spectypes.OperatorID {
		return share2.Committee[0].Signer
	})

	t.Run("check initial store state", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
		require.Len(t, selfStore.SelfValidators(), 0)
		require.Len(t, selfStore.SelfCommittees(), 0)
	})

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	require.NoError(t, store.handleSharesAdded(share1, share2))

	t.Run("check added shares", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), share2)

		require.Len(t, store.Committees(), 2)
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share2}))

		s1, e := store.Validator(share1.ValidatorPubKey[:])
		require.True(t, e)
		require.Equal(t, share1, s1)
		s2, e := store.Validator(share2.ValidatorPubKey[:])
		require.True(t, e)
		require.Equal(t, share2, s2)

		c1, e := store.Committee(share1.CommitteeID())
		require.True(t, e)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), c1)

		c2, e := store.Committee(share2.CommitteeID())
		require.True(t, e)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share2}), c2)

		s1, e = store.ValidatorByIndex(share1.ValidatorIndex)
		require.True(t, e)
		require.Equal(t, share1, s1)
		s2, e = store.ValidatorByIndex(share2.ValidatorIndex)
		require.True(t, e)
		require.Equal(t, share2, s2)

		validatorIndex, e := store.ValidatorIndex(share1.ValidatorPubKey)
		require.True(t, e)
		require.Equal(t, share1.ValidatorIndex, validatorIndex)

		validatorIndex, e = store.ValidatorIndex(share2.ValidatorPubKey)
		require.True(t, e)
		require.Equal(t, share2.ValidatorIndex, validatorIndex)

		var invalidPK spectypes.ValidatorPK
		_, e = store.ValidatorIndex(invalidPK)
		require.False(t, e)

		require.Empty(t, store.ParticipatingValidators(99))
		require.Len(t, store.ParticipatingValidators(101), 1)
		require.Equal(t, share1, store.ParticipatingValidators(101)[0])
		require.Len(t, store.ParticipatingValidators(201), 2)
		require.Contains(t, store.ParticipatingValidators(201), share1)
		require.Contains(t, store.ParticipatingValidators(201), share2)

		require.Empty(t, store.ParticipatingCommittees(99))
		require.Len(t, store.ParticipatingCommittees(101), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.ParticipatingCommittees(101)[0])
		require.Len(t, store.ParticipatingCommittees(201), 2)
		require.Contains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.Contains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{share2}))

		require.Len(t, store.OperatorValidators(1), 1)
		require.Equal(t, share1, store.OperatorValidators(1)[0])
		require.Len(t, store.OperatorValidators(2), 1)
		require.Equal(t, share2, store.OperatorValidators(2)[0])
		require.Len(t, store.OperatorCommittees(1), 1)
		require.Len(t, store.OperatorCommittees(2), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.OperatorCommittees(1)[0])
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share2}), store.OperatorCommittees(2)[0])

		require.Len(t, selfStore.SelfValidators(), 1)
		require.Equal(t, share2, selfStore.SelfValidators()[0])
		require.Len(t, selfStore.SelfCommittees(), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share2}), selfStore.SelfCommittees()[0])

		require.Empty(t, selfStore.SelfParticipatingValidators(99))
		require.Len(t, selfStore.SelfParticipatingValidators(201), 1)
		require.Contains(t, selfStore.SelfParticipatingValidators(201), share2)

		require.Empty(t, selfStore.SelfParticipatingCommittees(99))
		require.Len(t, selfStore.SelfParticipatingCommittees(201), 1)
		require.Contains(t, selfStore.SelfParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{share2}))
	})

	shareMap[share2.ValidatorPubKey] = updatedShare2
	require.NoError(t, store.handleSharesUpdated(updatedShare2))

	t.Run("check updated share", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), updatedShare2)

		require.Len(t, store.Committees(), 2)
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{updatedShare2}))

		s1, e := store.Validator(share1.ValidatorPubKey[:])
		require.True(t, e)
		require.Equal(t, share1, s1)
		s2, e := store.Validator(updatedShare2.ValidatorPubKey[:])
		require.True(t, e)
		require.Equal(t, updatedShare2, s2)

		c1, e := store.Committee(share1.CommitteeID())
		require.True(t, e)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), c1)

		c2, e := store.Committee(updatedShare2.CommitteeID())
		require.True(t, e)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{updatedShare2}), c2)

		s1, e = store.ValidatorByIndex(share1.ValidatorIndex)
		require.True(t, e)
		require.Equal(t, share1, s1)
		s2, e = store.ValidatorByIndex(updatedShare2.ValidatorIndex)
		require.True(t, e)
		require.Equal(t, updatedShare2, s2)

		require.Empty(t, store.ParticipatingValidators(99))
		require.Len(t, store.ParticipatingValidators(101), 1)
		require.Equal(t, share1, store.ParticipatingValidators(101)[0])
		require.Len(t, store.ParticipatingValidators(201), 1)
		require.Contains(t, store.ParticipatingValidators(201), share1)
		require.NotContains(t, store.ParticipatingValidators(201), updatedShare2)

		require.Empty(t, store.ParticipatingCommittees(99))
		require.Len(t, store.ParticipatingCommittees(101), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.ParticipatingCommittees(101)[0])
		require.Len(t, store.ParticipatingCommittees(201), 1)
		require.Contains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.NotContains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{updatedShare2}))

		require.Len(t, store.OperatorValidators(1), 1)
		require.Equal(t, share1, store.OperatorValidators(1)[0])
		require.Len(t, store.OperatorValidators(2), 1)
		require.Equal(t, updatedShare2, store.OperatorValidators(2)[0])
		require.Len(t, store.OperatorCommittees(1), 1)
		require.Len(t, store.OperatorCommittees(2), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.OperatorCommittees(1)[0])
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{updatedShare2}), store.OperatorCommittees(2)[0])

		require.Len(t, selfStore.SelfValidators(), 1)
		require.Equal(t, updatedShare2, selfStore.SelfValidators()[0])
		require.Len(t, selfStore.SelfCommittees(), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{updatedShare2}), selfStore.SelfCommittees()[0])

		require.Empty(t, selfStore.SelfParticipatingValidators(99))
		require.Len(t, selfStore.SelfParticipatingValidators(201), 0)
		require.NotContains(t, selfStore.SelfParticipatingValidators(201), updatedShare2)

		require.Empty(t, selfStore.SelfParticipatingCommittees(99))
		require.Len(t, selfStore.SelfParticipatingCommittees(201), 0)
		require.NotContains(t, selfStore.SelfParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{updatedShare2}))
	})

	require.NoError(t, store.handleShareRemoved(share2))
	delete(shareMap, share2.ValidatorPubKey)

	t.Run("check removed share", func(t *testing.T) {
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), share1)

		require.Len(t, store.Committees(), 1)
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share1}))

		s, e := store.Validator(share1.ValidatorPubKey[:])
		require.True(t, e)
		require.Equal(t, share1, s)

		c1, e := store.Committee(share1.CommitteeID())
		require.True(t, e)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), c1)

		s, e = store.ValidatorByIndex(share1.ValidatorIndex)
		require.True(t, e)
		require.Equal(t, share1, s)

		require.Empty(t, store.ParticipatingValidators(99))
		require.Len(t, store.ParticipatingValidators(101), 1)
		require.Equal(t, share1, store.ParticipatingValidators(101)[0])
		require.Len(t, store.ParticipatingValidators(201), 1)
		require.Contains(t, store.ParticipatingValidators(201), share1)

		require.Empty(t, store.ParticipatingCommittees(99))
		require.Len(t, store.ParticipatingCommittees(101), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.ParticipatingCommittees(101)[0])
		require.Len(t, store.ParticipatingCommittees(201), 1)
		require.Contains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{share1}))

		require.Len(t, store.OperatorValidators(1), 1)
		require.Equal(t, share1, store.OperatorValidators(1)[0])
		require.Len(t, store.OperatorValidators(2), 0)

		require.Len(t, store.OperatorCommittees(1), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.OperatorCommittees(1)[0])
		require.Len(t, store.OperatorCommittees(2), 0)

		require.Len(t, selfStore.SelfValidators(), 0)
		require.Len(t, selfStore.SelfCommittees(), 0)

		require.Empty(t, selfStore.SelfParticipatingValidators(99))
		require.Empty(t, selfStore.SelfParticipatingValidators(201))

		require.Empty(t, selfStore.SelfParticipatingCommittees(99))
		require.Empty(t, selfStore.SelfParticipatingCommittees(201))
	})

	store.handleDrop()
	for key := range shareMap {
		delete(shareMap, key)
	}

	t.Run("check drop", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
		require.Len(t, selfStore.SelfValidators(), 0)
		require.Len(t, selfStore.SelfCommittees(), 0)
	})
}

// Additional test to ensure full state drop functionality is correct
func TestValidatorStore_DropState(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	require.NoError(t, store.handleSharesAdded(share1, share2))

	t.Run("state before drop", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Len(t, store.Committees(), 2)
	})

	// Perform drop
	store.handleDrop()
	for key := range shareMap {
		delete(shareMap, key)
	}

	t.Run("state after drop", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)

		s, e := store.Validator(share1.ValidatorPubKey[:])
		require.False(t, e)
		require.Nil(t, s)

		s, e = store.Validator(share2.ValidatorPubKey[:])
		require.False(t, e)
		require.Nil(t, s)
	})
}

func TestValidatorStore_Concurrency(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	require.NoError(t, store.handleSharesAdded(share1, share2))

	var wg sync.WaitGroup
	wg.Add(6)

	go func() {
		defer wg.Done()
		store.Validator(share1.ValidatorPubKey[:])
	}()
	go func() {
		defer wg.Done()
		store.Validator(share2.ValidatorPubKey[:])
	}()
	go func() {
		defer wg.Done()
		store.Committee(share1.CommitteeID())
	}()
	go func() {
		defer wg.Done()
		store.Committee(share2.CommitteeID())
	}()
	go func() {
		defer wg.Done()
		store.Validators()
	}()
	go func() {
		defer wg.Done()
		store.Committees()
	}()

	wg.Wait()
}

func TestSelfValidatorStore_NilOperatorID(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	require.NoError(t, store.handleSharesAdded(share1, share2))

	selfStore := store.WithOperatorID(nil)
	require.Nil(t, selfStore.SelfValidators())
	require.Nil(t, selfStore.SelfCommittees())
	require.Nil(t, selfStore.SelfParticipatingValidators(99))
	require.Nil(t, selfStore.SelfParticipatingValidators(201))
	require.Nil(t, selfStore.SelfParticipatingCommittees(99))
	require.Nil(t, selfStore.SelfParticipatingCommittees(201))
}

func BenchmarkValidatorStore_Add(b *testing.B) {
	shares := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	const (
		totalOperators  = 500
		totalValidators = 50_000
	)

	var validatorIndex atomic.Int64
	createShare := func(operators []spectypes.OperatorID) *ssvtypes.SSVShare {
		index := validatorIndex.Add(1)

		var pk spectypes.ValidatorPK
		binary.LittleEndian.PutUint64(pk[:], uint64(index))

		var committee []*spectypes.ShareMember
		for _, signer := range operators {
			committee = append(committee, &spectypes.ShareMember{Signer: signer})
		}

		return &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  phase0.ValidatorIndex(index),
				ValidatorPubKey: pk,
				SharePubKey:     pk[:],
				Committee:       committee,
				Graffiti:        []byte("example"),
			},
		}
	}

	for i := 0; i < totalValidators; i++ {
		committee := make([]spectypes.OperatorID, 4)
		if rand.Float64() < 0.02 {
			// 2% chance of a purely random committee.
			for i, id := range rand.Perm(totalOperators)[:4] {
				committee[i] = spectypes.OperatorID(id)
			}
		} else {
			// 98% chance to form big committees.
			first := rand.Intn(totalOperators * 0.2) // 20% of the operators.
			for i := range committee {
				committee[i] = spectypes.OperatorID((first + i) % totalOperators)
			}
		}
		share := createShare(committee)
		shares[share.ValidatorPubKey] = share
	}

	// Print table of committees and validator counts for debugging.
	committees := map[[4]spectypes.OperatorID]int{}
	for _, share := range shares {
		committee := [4]spectypes.OperatorID{}
		for i, member := range share.Committee {
			committee[i] = member.Signer
		}
		committees[committee]++
	}
	tbl := table.New(os.Stdout)
	tbl.SetHeaders("Committee", "Validators")
	for committee, count := range committees {
		tbl.AddRow(fmt.Sprintf("%v", committee), fmt.Sprintf("%d", count))
	}
	// tbl.Render() // Uncomment to print.

	b.Logf("Total committees: %d", len(committees))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		store := createValidatorStore(shares)
		b.StartTimer()

		keys := slices.Collect(maps.Keys(shares))
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(start, end int) {
				defer wg.Done()
				for i := range keys[start:end] {
					require.NoError(b, store.handleSharesAdded(shares[keys[i]]))
				}
			}(i*(len(shares)/10), (i+1)*(len(shares)/10))
		}
		wg.Wait()
	}
}

func BenchmarkValidatorStore_Update(b *testing.B) {
	shares := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	const (
		totalOperators  = 500
		totalValidators = 50_000
	)

	var validatorIndex atomic.Int64
	createShare := func(operators []spectypes.OperatorID) *ssvtypes.SSVShare {
		index := validatorIndex.Add(1)

		var pk spectypes.ValidatorPK
		binary.LittleEndian.PutUint64(pk[:], uint64(index))

		var committee []*spectypes.ShareMember
		for _, signer := range operators {
			committee = append(committee, &spectypes.ShareMember{Signer: signer})
		}

		return &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  phase0.ValidatorIndex(index),
				ValidatorPubKey: pk,
				SharePubKey:     pk[:],
				Committee:       committee,
				Graffiti:        []byte("example"),
			},
		}
	}

	for i := 0; i < totalValidators; i++ {
		committee := make([]spectypes.OperatorID, 4)
		if rand.Float64() < 0.02 {
			// 2% chance of a purely random committee.
			for i, id := range rand.Perm(totalOperators)[:4] {
				committee[i] = spectypes.OperatorID(id)
			}
		} else {
			// 98% chance to form big committees.
			first := rand.Intn(totalOperators * 0.2) // 20% of the operators.
			for i := range committee {
				committee[i] = spectypes.OperatorID((first + i) % totalOperators)
			}
		}
		share := createShare(committee)
		shares[share.ValidatorPubKey] = share
	}

	// Print table of committees and validator counts for debugging.
	committees := map[[4]spectypes.OperatorID]int{}
	for _, share := range shares {
		committee := [4]spectypes.OperatorID{}
		for i, member := range share.Committee {
			committee[i] = member.Signer
		}
		committees[committee]++
	}
	tbl := table.New(os.Stdout)
	tbl.SetHeaders("Committee", "Validators")
	for committee, count := range committees {
		tbl.AddRow(fmt.Sprintf("%v", committee), fmt.Sprintf("%d", count))
	}
	// tbl.Render() // Uncomment to print.

	b.Logf("Total committees: %d", len(committees))

	store := createValidatorStore(shares)
	require.NoError(b, store.handleSharesAdded(slices.Collect(maps.Values(shares))...))

	pubKeys := slices.Collect(maps.Keys(shares))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		randomShares := make([]*ssvtypes.SSVShare, 500)
		first := rand.Intn(len(pubKeys))
		for j := 0; j < 500; j++ {
			randomShares[j] = shares[pubKeys[(first+j)%len(pubKeys)]]
		}

		require.NoError(b, store.handleSharesUpdated(randomShares...))
	}
}

// Test for error handling and nil states
func TestValidatorStore_HandleNilAndEmptyStates(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := createValidatorStore(shareMap)

	// Attempt to remove a non-existing share
	t.Run("remove non-existing share", func(t *testing.T) {
		err := store.handleShareRemoved(&ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK{99, 88, 77},
			},
		})
		require.ErrorContains(t, err, "committee not found")
		// Ensure store remains unaffected
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})

	// Add nil share - this should be a no-op or handled gracefully
	t.Run("add nil share", func(t *testing.T) {
		require.ErrorContains(t, store.handleSharesAdded(nil), "nil share")
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})

	// Update nil share - this should be a no-op or handled gracefully
	t.Run("update nil share", func(t *testing.T) {
		require.ErrorContains(t, store.handleSharesUpdated(nil), "nil share")
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})

	// Delete nil share - this should be a no-op or handled gracefully
	t.Run("delete nil share", func(t *testing.T) {
		require.ErrorContains(t, store.handleShareRemoved(nil), "nil share")
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})
}

func TestValidatorStore_EmptyStoreOperations(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	// Correctly sized pubKey array for testing
	var pubKey spectypes.ValidatorPK
	copy(pubKey[:], []byte{1, 2, 3}) // Populate with test data; remaining bytes are zeroed

	t.Run("validate empty store operations", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
		s, e := store.Validator(pubKey[:])
		require.False(t, e)
		require.Nil(t, s)
		c, e := store.Committee(spectypes.CommitteeID{1})
		require.False(t, e)
		require.Nil(t, c)
		require.Len(t, store.OperatorValidators(1), 0)
		require.Len(t, store.OperatorCommittees(1), 0)
	})
}

func TestValidatorStore_AddDuplicateShares(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	shareMap[share1.ValidatorPubKey] = share1
	require.NoError(t, store.handleSharesAdded(share1))

	t.Run("validate store after adding duplicate shares", func(t *testing.T) {
		err := store.handleSharesAdded(share1)
		require.ErrorContains(t, err, "share already exists in committee")
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), share1)
	})
}

func TestValidatorStore_UpdateNonExistingShare(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	t.Run("update non-existing share", func(t *testing.T) {
		require.NotPanics(t, func() {
			err := store.handleSharesUpdated(share1)
			require.ErrorContains(t, err, "committee not found")
		})
		require.Len(t, store.Validators(), 0)

		s, e := store.Validator(share1.ValidatorPubKey[:])
		require.False(t, e)
		require.Nil(t, s)
	})
}

func TestValidatorStore_RemoveNonExistingShare(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	t.Run("remove non-existing share", func(t *testing.T) {
		require.NoError(t, store.handleSharesAdded(share1)) // Remove without adding
		require.Len(t, store.Validators(), 0)

		s, e := store.Validator(share1.ValidatorPubKey[:])
		require.False(t, e)
		require.Nil(t, s)
	})
}

func TestValidatorStore_HandlingDifferentStatuses(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	share3 := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  phase0.ValidatorIndex(3),
			ValidatorPubKey: spectypes.ValidatorPK{10, 11, 12},
			SharePubKey:     spectypes.ShareValidatorPK{13, 14, 15},
			Committee:       []*spectypes.ShareMember{{Signer: 3}},
			Graffiti:        []byte("status_test"),
		},
		ActivationEpoch: 300,
		ExitEpoch:       goclient.FarFutureEpoch,
		Status:          eth2apiv1.ValidatorStateActiveOngoing,
		OwnerAddress:    common.HexToAddress("0xabcde"),
		Liquidated:      false,
	}

	store := createValidatorStore(shareMap)

	shareMap[share3.ValidatorPubKey] = share3
	require.NoError(t, store.handleSharesAdded(share3))

	t.Run("check shares with different statuses", func(t *testing.T) {
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), share3)

		require.Len(t, store.ParticipatingValidators(301), 1) // Active status should be participating
		require.Equal(t, share3, store.ParticipatingValidators(301)[0])
	})
}

func TestValidatorStore_AddRemoveBulkShares(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	// Create a large number of shares
	var bulkShares []*ssvtypes.SSVShare
	for i := 0; i < 100; i++ {
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  phase0.ValidatorIndex(i),
				ValidatorPubKey: spectypes.ValidatorPK{byte(i), byte(i + 1), byte(i + 2)},
				SharePubKey:     spectypes.ShareValidatorPK{byte(i + 3), byte(i + 4), byte(i + 5)},
				Committee:       []*spectypes.ShareMember{{Signer: spectypes.OperatorID(i % 5)}},
				Graffiti:        []byte("bulk_add"),
			},
			ActivationEpoch: phase0.Epoch(i),
			ExitEpoch:       goclient.FarFutureEpoch,
			Status:          eth2apiv1.ValidatorStatePendingQueued,
			OwnerAddress:    common.HexToAddress(fmt.Sprintf("0x%x", i)),
			Liquidated:      false,
		}
		bulkShares = append(bulkShares, share)
		shareMap[share.ValidatorPubKey] = share
	}

	require.NoError(t, store.handleSharesAdded(bulkShares...))

	t.Run("check bulk added shares", func(t *testing.T) {
		require.Len(t, store.Validators(), 100)
		for _, share := range bulkShares {
			require.Contains(t, store.Validators(), share)
		}
	})

	// Remove all shares
	for _, share := range bulkShares {
		require.NoError(t, store.handleShareRemoved(share))
		delete(shareMap, share.ValidatorPubKey)
	}

	t.Run("check state after bulk removal", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})
}

func TestValidatorStore_MixedOperations(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	// Initial adds
	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	require.NoError(t, store.handleSharesAdded(share1, share2))

	// Mixed operations
	shareMap[share1.ValidatorPubKey] = share1 // Re-add share1
	err := store.handleSharesAdded(share1)
	require.ErrorContains(t, err, "share already exists in committee")

	shareMap[updatedShare2.ValidatorPubKey] = updatedShare2
	require.NoError(t, store.handleSharesUpdated(updatedShare2)) // Update share2

	t.Run("check mixed operations result", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), updatedShare2)
	})
}

func TestValidatorStore_InvalidCommitteeHandling(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	invalidCommitteeShare := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  phase0.ValidatorIndex(10),
			ValidatorPubKey: spectypes.ValidatorPK{10, 20, 30},
			SharePubKey:     spectypes.ShareValidatorPK{40, 50, 60},
			// Invalid committee with duplicate members.
			// This scenario is included for testing purposes only, as the event handler should validate and prevent duplicate members.
			Committee: []*spectypes.ShareMember{{Signer: 1}, {Signer: 1}}, // Duplicate members
			Graffiti:  []byte("invalid_committee"),
		},
		ActivationEpoch: 500,
		ExitEpoch:       goclient.FarFutureEpoch,
		Status:          eth2apiv1.ValidatorStatePendingQueued,
		OwnerAddress:    common.HexToAddress("0xdeadbeef"),
		Liquidated:      false,
	}

	shareMap[invalidCommitteeShare.ValidatorPubKey] = invalidCommitteeShare
	err := store.handleSharesAdded(invalidCommitteeShare)
	require.ErrorContains(t, err, "duplicate operator in share. operator_id=1")
}

func TestValidatorStore_BulkAddUpdate(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	// Initial shares to add
	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2

	t.Run("bulk add shares", func(t *testing.T) {
		require.NoError(t, store.handleSharesAdded(share1, share2))
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), share2)
	})

	// Update shares
	share1.Status = eth2apiv1.ValidatorStateActiveOngoing
	share2.Status = eth2apiv1.ValidatorStateActiveOngoing

	t.Run("bulk update shares", func(t *testing.T) {
		require.NoError(t, store.handleSharesUpdated(share1, share2))
		s1, e1 := store.Validator(share1.ValidatorPubKey[:])
		s2, e2 := store.Validator(share2.ValidatorPubKey[:])
		require.True(t, e1)
		require.True(t, e2)
		require.Equal(t, eth2apiv1.ValidatorStateActiveOngoing, s1.Status)
		require.Equal(t, eth2apiv1.ValidatorStateActiveOngoing, s2.Status)
	})
}

func TestValidatorStore_ComprehensiveIndex(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := createValidatorStore(shareMap)

	// Share without metadata
	noMetadataShare := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  0,
			ValidatorPubKey: spectypes.ValidatorPK{13, 14, 15},
			Committee:       []*spectypes.ShareMember{{Signer: 3}},
		},
	}

	shareMap[noMetadataShare.ValidatorPubKey] = noMetadataShare

	t.Run("add share with no metadata", func(t *testing.T) {
		require.NoError(t, store.handleSharesAdded(noMetadataShare))

		s, e := store.ValidatorByIndex(0)
		require.False(t, e)
		require.Nil(t, s)

		require.Len(t, store.OperatorValidators(3), 1)
		require.Equal(t, noMetadataShare, store.OperatorValidators(3)[0])
	})

	// Update share to have metadata
	noMetadataShare.ValidatorIndex = phase0.ValidatorIndex(10)
	noMetadataShare.Status = eth2apiv1.ValidatorStatePendingInitialized

	t.Run("update share with metadata", func(t *testing.T) {
		require.NoError(t, store.handleSharesUpdated(noMetadataShare))

		s, e := store.ValidatorByIndex(10)
		require.True(t, e)
		require.Equal(t, noMetadataShare, s)
	})

	t.Run("remove share", func(t *testing.T) {
		require.NoError(t, store.handleShareRemoved(noMetadataShare))
		// Remove from shareMap to mimic actual behavior
		delete(shareMap, noMetadataShare.ValidatorPubKey)

		// Ensure complete removal
		s, e := store.ValidatorByIndex(10)
		require.False(t, e)
		require.Nil(t, s, "Validator by index should be nil after removal")
		require.Empty(t, store.OperatorValidators(3), "Operator validators should be empty after removal")

		s, e = store.Validator(noMetadataShare.ValidatorPubKey[:])
		require.False(t, e)
		require.Nil(t, s, "Validator should be nil after removal")
	})
}

func TestValidatorStore_HandleDuplicateSharesAdded(t *testing.T) {
	store := createValidatorStore(map[spectypes.ValidatorPK]*ssvtypes.SSVShare{})

	// Create a share
	duplicateShare := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  phase0.ValidatorIndex(1),
			ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
			SharePubKey:     spectypes.ShareValidatorPK{4, 5, 6},
			Committee:       []*spectypes.ShareMember{{Signer: 1}},
			Graffiti:        []byte("duplicate_test"),
		},
		ActivationEpoch: 100,
		ExitEpoch:       goclient.FarFutureEpoch,
		Status:          eth2apiv1.ValidatorStatePendingQueued,
		OwnerAddress:    common.HexToAddress("0x12345"),
		Liquidated:      false,
	}

	// Add the same share multiple times
	require.NoError(t, store.handleSharesAdded(duplicateShare))

	err := store.handleSharesAdded(duplicateShare)
	require.ErrorContains(t, err, "share already exists in committee")

	t.Run("check no duplicates in data.shares", func(t *testing.T) {
		// Validate the internal state for operator ID
		data, exists := store.byOperatorID[duplicateShare.Committee[0].Signer]
		require.True(t, exists, "operator data should exist")
		require.NotNil(t, data, "operator data should not be nil")

		// Ensure no duplicates in shares
		require.Len(t, data.shares, 1, "data.shares should not contain duplicate entries")
		require.Contains(t, data.shares, duplicateShare, "data.shares should contain the added share")
	})

	t.Run("check no duplicates in committee validators", func(t *testing.T) {
		committeeID := duplicateShare.CommitteeID()
		committee, exists := store.byCommitteeID[committeeID]
		require.True(t, exists, "committee should exist")
		require.NotNil(t, committee, "committee should not be nil")

		// Ensure no duplicates in committee validators
		require.Len(t, committee.Shares, 1, "committee.Validators should not contain duplicate entries")
		require.Contains(t, committee.Shares, duplicateShare, "committee.Validators should contain the added share")
	})
}

// requireValidatorStoreIntegrity checks that every function of the ValidatorStore returns the expected results,
// by reconstructing the expected state from the given shares and comparing it to the actual state of the store.
// This may seem like an overkill, but as ValidatorStore's implementation becomes more optimized and complex,
// it's a good way to double-check it with a dumb implementation that never changes.
func requireValidatorStoreIntegrity(t *testing.T, store ValidatorStore, shares []*ssvtypes.SSVShare) {
	// Check that there are no false positives.
	const nonExistingIndex = phase0.ValidatorIndex(math.MaxUint64 - 1)
	const nonExistingOperatorID = spectypes.OperatorID(math.MaxUint64 - 1)
	var nonExistingCommitteeID spectypes.CommitteeID
	n, err := cryptorand.Read(nonExistingCommitteeID[:])
	require.NoError(t, err)
	require.Equal(t, len(nonExistingCommitteeID), n)

	validator, exists := store.ValidatorByIndex(nonExistingIndex)
	require.False(t, exists)
	require.Nil(t, validator)

	operatorShares := store.OperatorValidators(nonExistingOperatorID)
	require.Empty(t, operatorShares)

	committee, exists := store.Committee(nonExistingCommitteeID)
	require.False(t, exists)
	require.Nil(t, committee)

	// Check Validator(pubkey) and ValidatorByIndex(index)
	for _, share := range shares {
		byPubKey, exists := store.Validator(share.ValidatorPubKey[:])
		require.True(t, exists, "validator %x not found", share.ValidatorPubKey)
		requireEqualShare(t, share, byPubKey)

		byIndex, exists := store.ValidatorByIndex(share.ValidatorIndex)
		require.True(t, exists, "validator %d not found", share.ValidatorIndex)
		requireEqualShare(t, share, byIndex)
	}

	// Reconstruct hierarchy to check integrity of operators and committees.
	byOperator := make(map[spectypes.OperatorID][]*ssvtypes.SSVShare)
	byCommittee := make(map[spectypes.CommitteeID][]*ssvtypes.SSVShare)
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

	// Check OperatorValidators(operatorID)
	for operatorID, shares := range byOperator {
		operatorShares := store.OperatorValidators(operatorID)
		requireEqualShares(t, shares, operatorShares)
	}

	// Check Committees(cmtID)
	for cmtID, shares := range byCommittee {
		cmt, exists := store.Committee(cmtID)
		require.True(t, exists)
		requireEqualShares(t, shares, cmt.Shares)

		operatorIDs := slices.Collect(maps.Keys(committeeOperators[cmtID]))
		slices.Sort(operatorIDs)
		require.Equal(t, operatorIDs, cmt.Operators, "committee %s has %d operators, but %d in store", cmtID, len(operatorIDs), len(cmt.Operators))
	}

	// Check Committees()
	storeCommittees := store.Committees()
	require.Equal(t, len(byCommittee), len(storeCommittees))
	for _, cmt := range storeCommittees {
		_, ok := byCommittee[cmt.ID]
		require.True(t, ok)
	}

	// Check Validators()
	storeValidators := store.Validators()
	require.Equal(t, len(shares), len(storeValidators))
	for _, share := range shares {
		storeIndex := slices.IndexFunc(storeValidators, func(validator *ssvtypes.SSVShare) bool {
			return validator.ValidatorPubKey == share.ValidatorPubKey
		})
		require.NotEqual(t, -1, storeIndex)
		requireEqualShare(t, share, storeValidators[storeIndex])
	}

	// Check OperatorCommittees(operatorID)
	for operatorID, committees := range operatorCommittees {
		storeOperatorCommittees := store.OperatorCommittees(operatorID)
		require.Equal(t, len(committees), len(storeOperatorCommittees), "operator %d has %d committees, but %d in store", operatorID, len(committees), len(storeOperatorCommittees))
		for committee := range committees {
			// Find the committee in the store.
			storeIndex := slices.IndexFunc(storeOperatorCommittees, func(storeCommittee *Committee) bool {
				return storeCommittee.ID == committee
			})
			require.NotEqual(t, -1, storeIndex)

			// Compare shares.
			storeOperatorCommittee := storeOperatorCommittees[storeIndex]
			requireEqualShares(t, byCommittee[committee], storeOperatorCommittee.Shares, "committee %v doesn't have expected shares", storeOperatorCommittee.Operators)

			// Compare operator IDs.
			operatorIDs := slices.Collect(maps.Keys(committeeOperators[committee]))
			slices.Sort(operatorIDs)
			require.Equal(t, operatorIDs, storeOperatorCommittee.Operators)

			// Compare indices.
			expectedIndices := make([]phase0.ValidatorIndex, len(storeOperatorCommittee.Shares))
			for i, validator := range storeOperatorCommittee.Shares {
				expectedIndices[i] = validator.ValidatorIndex
			}
			slices.Sort(expectedIndices)
			storeIndices := make([]phase0.ValidatorIndex, len(storeOperatorCommittee.Shares))
			copy(storeIndices, storeOperatorCommittee.Indices)
			slices.Sort(storeIndices)
			require.Equal(t, expectedIndices, storeIndices)
		}
	}

	// Check ParticipatingValidators(epoch)
	var epoch = phase0.Epoch(math.MaxUint64 - 1)
	var participatingValidators []*ssvtypes.SSVShare
	var participatingCommittees = make(map[spectypes.CommitteeID]struct{})
	for _, share := range shares {
		if share.IsParticipating(networkConfig.Beacon, epoch) {
			participatingValidators = append(participatingValidators, share)
			participatingCommittees[share.CommitteeID()] = struct{}{}
		}
	}
	storeParticipatingValidators := store.ParticipatingValidators(epoch)
	requireEqualShares(t, participatingValidators, storeParticipatingValidators)

	// Check ParticipatingCommittees(epoch)
	storeParticipatingCommittees := store.ParticipatingCommittees(epoch)
	require.Equal(t, len(participatingCommittees), len(storeParticipatingCommittees))
	for _, cmt := range storeParticipatingCommittees {
		_, ok := participatingCommittees[cmt.ID]
		require.True(t, ok)
	}
}

func createValidatorStore(shares map[spectypes.ValidatorPK]*ssvtypes.SSVShare) *validatorStore {
	return newValidatorStore(
		func() []*ssvtypes.SSVShare { return slices.Collect(maps.Values(shares)) },
		func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
			share := shares[spectypes.ValidatorPK(pubKey)]
			if share == nil {
				return nil, false
			}
			return share, true
		},
		func(owner common.Address) (bellatrix.ExecutionAddress, error) {
			// For tests, just return owner address as fee recipient
			return bellatrix.ExecutionAddress(owner), nil
		},
		make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
		networkConfig.Beacon,
	)
}

// TestValidatorStore_FeeRecipients tests the fee recipient functionality
func TestValidatorStore_FeeRecipients(t *testing.T) {
	t.Run("GetFeeRecipient with custom recipient", func(t *testing.T) {
		owner1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
		owner2 := common.HexToAddress("0x2345678901234567890123456789012345678901")
		customRecipient1 := bellatrix.ExecutionAddress{0x11, 0x22, 0x33}
		customRecipient2 := bellatrix.ExecutionAddress{0x44, 0x55, 0x66}

		testShares := []*ssvtypes.SSVShare{
			{
				Share: spectypes.Share{
					ValidatorIndex:  1,
					ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
					Committee:       []*spectypes.ShareMember{{Signer: 1}},
				},
				Status:       eth2apiv1.ValidatorStateActiveOngoing,
				OwnerAddress: owner1,
			},
			{
				Share: spectypes.Share{
					ValidatorIndex:  2,
					ValidatorPubKey: spectypes.ValidatorPK{4, 5, 6},
					Committee:       []*spectypes.ShareMember{{Signer: 1}},
				},
				Status:       eth2apiv1.ValidatorStateActiveOngoing,
				OwnerAddress: owner2,
			},
		}

		feeRecipientByOwner := func(owner common.Address) (bellatrix.ExecutionAddress, error) {
			if owner == owner1 {
				return customRecipient1, nil
			}
			if owner == owner2 {
				return customRecipient2, nil
			}
			return bellatrix.ExecutionAddress{}, fmt.Errorf("no fee recipient for owner %s", owner.Hex())
		}

		shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{
			testShares[0].ValidatorPubKey: testShares[0],
			testShares[1].ValidatorPubKey: testShares[1],
		}

		store := newValidatorStore(
			func() []*ssvtypes.SSVShare { return slices.Collect(maps.Values(shareMap)) },
			func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
				s := shareMap[spectypes.ValidatorPK(pubKey)]
				if s == nil {
					return nil, false
				}
				return s, true
			},
			feeRecipientByOwner,
			make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
			networkConfig.Beacon,
		)

		require.NoError(t, store.handleSharesAdded(testShares[0], testShares[1]))

		// Test GetFeeRecipient returns custom recipients
		recipient1, err := store.GetFeeRecipient(testShares[0].ValidatorPubKey)
		require.NoError(t, err)
		require.Equal(t, customRecipient1, recipient1)

		recipient2, err := store.GetFeeRecipient(testShares[1].ValidatorPubKey)
		require.NoError(t, err)
		require.Equal(t, customRecipient2, recipient2)

		// Test non-existent validator
		_, err = store.GetFeeRecipient(spectypes.ValidatorPK{99, 99, 99})
		require.Error(t, err)
	})

	t.Run("GetFeeRecipient returns error when no custom recipient", func(t *testing.T) {
		owner := common.HexToAddress("0x1234567890123456789012345678901234567890")

		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
				Committee:       []*spectypes.ShareMember{{Signer: 1}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: owner,
		}

		// No custom fee recipient set
		feeRecipientByOwner := func(owner common.Address) (bellatrix.ExecutionAddress, error) {
			return bellatrix.ExecutionAddress{}, fmt.Errorf("no fee recipient for owner %s", owner.Hex())
		}

		shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{
			share.ValidatorPubKey: share,
		}

		store := newValidatorStore(
			func() []*ssvtypes.SSVShare { return slices.Collect(maps.Values(shareMap)) },
			func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
				s := shareMap[spectypes.ValidatorPK(pubKey)]
				if s == nil {
					return nil, false
				}
				return s, true
			},
			feeRecipientByOwner,
			make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
			networkConfig.Beacon,
		)

		require.NoError(t, store.handleSharesAdded(share))

		// Test GetFeeRecipient returns error when no custom recipient is set
		_, err := store.GetFeeRecipient(share.ValidatorPubKey)
		require.Error(t, err, "Should error when no fee recipient is configured")
		require.Contains(t, err.Error(), "no fee recipient for owner")
		require.Contains(t, err.Error(), owner.Hex())
	})

	t.Run("GetFeeRecipient with no custom recipient returns error", func(t *testing.T) {
		owner := common.HexToAddress("0x1234567890123456789012345678901234567890")

		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
				Committee:       []*spectypes.ShareMember{{Signer: 1}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: owner,
		}

		shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{
			share.ValidatorPubKey: share,
		}

		store := newValidatorStore(
			func() []*ssvtypes.SSVShare { return slices.Collect(maps.Values(shareMap)) },
			func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
				s := shareMap[spectypes.ValidatorPK(pubKey)]
				if s == nil {
					return nil, false
				}
				return s, true
			},
			func(owner common.Address) (bellatrix.ExecutionAddress, error) {
				// Return error to test error propagation
				return bellatrix.ExecutionAddress{}, fmt.Errorf("no custom recipient")
			},
			make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
			networkConfig.Beacon,
		)

		require.NoError(t, store.handleSharesAdded(share))

		// Test GetFeeRecipient returns error when no custom recipient is set
		_, err := store.GetFeeRecipient(share.ValidatorPubKey)
		require.Error(t, err, "Should error when no fee recipient is configured")
		require.Contains(t, err.Error(), "no fee recipient for owner")
	})

	t.Run("GetFeeRecipient after share removal", func(t *testing.T) {
		owner := common.HexToAddress("0x1234567890123456789012345678901234567890")
		customRecipient := bellatrix.ExecutionAddress{0x11, 0x22, 0x33}

		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
				Committee:       []*spectypes.ShareMember{{Signer: 1}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: owner,
		}

		feeRecipientByOwner := func(owner common.Address) (bellatrix.ExecutionAddress, error) {
			return customRecipient, nil
		}

		shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{
			share.ValidatorPubKey: share,
		}

		store := newValidatorStore(
			func() []*ssvtypes.SSVShare { return slices.Collect(maps.Values(shareMap)) },
			func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
				s := shareMap[spectypes.ValidatorPK(pubKey)]
				if s == nil {
					return nil, false
				}
				return s, true
			},
			feeRecipientByOwner,
			make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
			networkConfig.Beacon,
		)

		require.NoError(t, store.handleSharesAdded(share))

		// Verify fee recipient is available
		recipient, err := store.GetFeeRecipient(share.ValidatorPubKey)
		require.NoError(t, err)
		require.Equal(t, customRecipient, recipient)

		// Remove the share
		delete(shareMap, share.ValidatorPubKey)
		require.NoError(t, store.handleShareRemoved(share))

		// Verify fee recipient is no longer available
		_, err = store.GetFeeRecipient(share.ValidatorPubKey)
		require.Error(t, err)
		// Error could be either "validator not found" or "validator share is nil" depending on implementation
		require.True(t, strings.Contains(err.Error(), "validator not found") || strings.Contains(err.Error(), "validator share is nil"),
			"Expected error about missing validator, got: %v", err)
	})

	t.Run("GetFeeRecipient concurrent access", func(t *testing.T) {
		owner := common.HexToAddress("0x1234567890123456789012345678901234567890")
		customRecipient := bellatrix.ExecutionAddress{0x11, 0x22, 0x33}

		// Create multiple shares for concurrent testing
		shares := make([]*ssvtypes.SSVShare, 10)
		shareMap := make(map[spectypes.ValidatorPK]*ssvtypes.SSVShare)

		for i := 0; i < 10; i++ {
			share := &ssvtypes.SSVShare{
				Share: spectypes.Share{
					ValidatorIndex:  phase0.ValidatorIndex(i),
					ValidatorPubKey: spectypes.ValidatorPK{byte(i), byte(i + 1), byte(i + 2)},
					Committee:       []*spectypes.ShareMember{{Signer: 1}},
				},
				Status:       eth2apiv1.ValidatorStateActiveOngoing,
				OwnerAddress: owner,
			}
			shares[i] = share
			shareMap[share.ValidatorPubKey] = share
		}

		feeRecipientByOwner := func(owner common.Address) (bellatrix.ExecutionAddress, error) {
			return customRecipient, nil
		}

		store := newValidatorStore(
			func() []*ssvtypes.SSVShare { return slices.Collect(maps.Values(shareMap)) },
			func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
				s := shareMap[spectypes.ValidatorPK(pubKey)]
				if s == nil {
					return nil, false
				}
				return s, true
			},
			feeRecipientByOwner,
			make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
			networkConfig.Beacon,
		)

		require.NoError(t, store.handleSharesAdded(shares...))

		// Test concurrent access to GetFeeRecipient
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				shareIdx := idx % 10
				recipient, err := store.GetFeeRecipient(shares[shareIdx].ValidatorPubKey)
				require.NoError(t, err)
				require.Equal(t, customRecipient, recipient)
			}(i)
		}
		wg.Wait()
	})

	t.Run("GetFeeRecipient with invalid validator index", func(t *testing.T) {
		owner := common.HexToAddress("0x1234567890123456789012345678901234567890")

		// Create a share but don't add it properly to internal maps
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:  1,
				ValidatorPubKey: spectypes.ValidatorPK{1, 2, 3},
				Committee:       []*spectypes.ShareMember{{Signer: 1}},
			},
			Status:       eth2apiv1.ValidatorStateActiveOngoing,
			OwnerAddress: owner,
		}

		// Create store with empty shareMap but corrupted internal state
		store := newValidatorStore(
			func() []*ssvtypes.SSVShare { return []*ssvtypes.SSVShare{} },
			func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
				return nil, false
			},
			func(owner common.Address) (bellatrix.ExecutionAddress, error) {
				return bellatrix.ExecutionAddress{}, fmt.Errorf("not found")
			},
			make(map[spectypes.ValidatorPK]phase0.ValidatorIndex),
			networkConfig.Beacon,
		)

		// Manually corrupt the internal state (simulate partial update)
		store.mu.Lock()
		store.pubkeyToIndex[share.ValidatorPubKey] = share.ValidatorIndex
		// Don't add to byValidatorIndex to simulate corruption
		store.mu.Unlock()

		// Should handle gracefully and return error
		_, err := store.GetFeeRecipient(share.ValidatorPubKey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "validator share is nil")
	})
}

// Ensures the index->pubkey mapping is permanent and accessible
// even after shares are removed or the transient state is dropped.
func TestValidatorStore_PermanentIndexToPubkeyMapping(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := createValidatorStore(shareMap)

	// Add a share
	share := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:  phase0.ValidatorIndex(42),
			ValidatorPubKey: spectypes.ValidatorPK{0xAA, 0xBB, 0xCC},
			Committee:       []*spectypes.ShareMember{{Signer: 1}},
		},
		Status:       eth2apiv1.ValidatorStateActiveOngoing,
		OwnerAddress: common.HexToAddress("0x1"),
	}

	shareMap[share.ValidatorPubKey] = share
	require.NoError(t, store.handleSharesAdded(share))

	// Mapping exists right after add
	pk, ok := store.ValidatorPubkey(share.ValidatorIndex)
	require.True(t, ok)
	require.Equal(t, share.ValidatorPubKey, pk)

	// Remove the share from store; mapping must remain
	delete(shareMap, share.ValidatorPubKey)
	require.NoError(t, store.handleShareRemoved(share))

	pk, ok = store.ValidatorPubkey(share.ValidatorIndex)
	require.True(t, ok)
	require.Equal(t, share.ValidatorPubKey, pk)

	// Drop transient state; mapping must remain
	store.handleDrop()

	pk, ok = store.ValidatorPubkey(share.ValidatorIndex)
	require.True(t, ok)
	require.Equal(t, share.ValidatorPubKey, pk)
}
