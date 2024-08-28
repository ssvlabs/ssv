package storage

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aquasecurity/table"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

var share1 = &ssvtypes.SSVShare{
	Share: spectypes.Share{
		ValidatorIndex:      phase0.ValidatorIndex(1),
		ValidatorPubKey:     spectypes.ValidatorPK{1, 2, 3},
		SharePubKey:         spectypes.ShareValidatorPK{4, 5, 6},
		Committee:           []*spectypes.ShareMember{{Signer: 1}},
		FeeRecipientAddress: [20]byte{10, 20, 30},
		Graffiti:            []byte("example"),
	},
	Metadata: ssvtypes.Metadata{
		BeaconMetadata: &beaconprotocol.ValidatorMetadata{
			Index:           phase0.ValidatorIndex(1),
			ActivationEpoch: 100,
			Status:          eth2apiv1.ValidatorStatePendingQueued,
		},
		OwnerAddress: common.HexToAddress("0x12345"),
		Liquidated:   false,
	},
}

var share2 = &ssvtypes.SSVShare{
	Share: spectypes.Share{
		ValidatorIndex:      phase0.ValidatorIndex(2),
		ValidatorPubKey:     spectypes.ValidatorPK{7, 8, 9},
		SharePubKey:         spectypes.ShareValidatorPK{10, 11, 12},
		Committee:           []*spectypes.ShareMember{{Signer: 2}},
		FeeRecipientAddress: [20]byte{40, 50, 60},
		Graffiti:            []byte("test"),
	},
	Metadata: ssvtypes.Metadata{
		BeaconMetadata: &beaconprotocol.ValidatorMetadata{
			Index:           phase0.ValidatorIndex(2),
			ActivationEpoch: 200,
			Status:          eth2apiv1.ValidatorStatePendingQueued,
		},
		OwnerAddress: common.HexToAddress("0x67890"),
		Liquidated:   false,
	},
}

var updatedShare2 = &ssvtypes.SSVShare{
	Share: spectypes.Share{
		ValidatorIndex:      phase0.ValidatorIndex(2),
		ValidatorPubKey:     spectypes.ValidatorPK{7, 8, 9},
		SharePubKey:         spectypes.ShareValidatorPK{10, 11, 12},
		Committee:           []*spectypes.ShareMember{{Signer: 2}},
		FeeRecipientAddress: [20]byte{40, 50, 60},
		Graffiti:            []byte("test"),
	},
	Metadata: ssvtypes.Metadata{
		BeaconMetadata: &beaconprotocol.ValidatorMetadata{
			Index:           phase0.ValidatorIndex(2),
			ActivationEpoch: 200,
			Status:          eth2apiv1.ValidatorStatePendingQueued,
		},
		OwnerAddress: common.HexToAddress("0x67890"),
		Liquidated:   true,
	},
}

func TestValidatorStore(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

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
	store.handleSharesAdded(share1, share2)

	t.Run("check added shares", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), share2)

		require.Len(t, store.Committees(), 2)
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share2}))

		require.Equal(t, share1, store.Validator(share1.ValidatorPubKey[:]))
		require.Equal(t, share2, store.Validator(share2.ValidatorPubKey[:]))

		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.Committee(share1.CommitteeID()))
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share2}), store.Committee(share2.CommitteeID()))

		require.Equal(t, share1, store.ValidatorByIndex(share1.ValidatorIndex))
		require.Equal(t, share2, store.ValidatorByIndex(share2.ValidatorIndex))

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
	store.handleSharesUpdated(updatedShare2)

	t.Run("check updated share", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), updatedShare2)

		require.Len(t, store.Committees(), 2)
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{updatedShare2}))

		require.Equal(t, share1, store.Validator(share1.ValidatorPubKey[:]))
		require.Equal(t, updatedShare2, store.Validator(updatedShare2.ValidatorPubKey[:]))

		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.Committee(share1.CommitteeID()))
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{updatedShare2}), store.Committee(updatedShare2.CommitteeID()))

		require.Equal(t, share1, store.ValidatorByIndex(share1.ValidatorIndex))
		require.Equal(t, updatedShare2, store.ValidatorByIndex(updatedShare2.ValidatorIndex))

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

	store.handleShareRemoved(share2.ValidatorPubKey)
	delete(shareMap, share2.ValidatorPubKey)

	t.Run("check removed share", func(t *testing.T) {
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), share1)

		require.Len(t, store.Committees(), 1)
		require.Contains(t, store.Committees(), buildCommittee([]*ssvtypes.SSVShare{share1}))

		require.Equal(t, share1, store.Validator(share1.ValidatorPubKey[:]))

		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.Committee(share1.CommitteeID()))

		require.Equal(t, share1, store.ValidatorByIndex(share1.ValidatorIndex))

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
	shareMap = map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

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

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	store.handleSharesAdded(share1, share2)

	t.Run("state before drop", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Len(t, store.Committees(), 2)
	})

	// Perform drop
	store.handleDrop()
	shareMap = map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	t.Run("state after drop", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
		require.Nil(t, store.Validator(share1.ValidatorPubKey[:]))
		require.Nil(t, store.Validator(share2.ValidatorPubKey[:]))
	})
}

func TestValidatorStore_Concurrency(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	store.handleSharesAdded(share1, share2)

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

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	store.handleSharesAdded(share1, share2)

	selfStore := store.WithOperatorID(nil)
	require.Nil(t, selfStore.SelfValidators())
	require.Nil(t, selfStore.SelfCommittees())
	require.Nil(t, selfStore.SelfParticipatingValidators(99))
	require.Nil(t, selfStore.SelfParticipatingValidators(201))
	require.Nil(t, selfStore.SelfParticipatingCommittees(99))
	require.Nil(t, selfStore.SelfParticipatingCommittees(201))
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
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index: phase0.ValidatorIndex(index),
				},
			},
			Share: spectypes.Share{
				ValidatorIndex:      phase0.ValidatorIndex(index),
				ValidatorPubKey:     pk,
				SharePubKey:         pk[:],
				Committee:           committee,
				FeeRecipientAddress: [20]byte{10, 20, 30},
				Graffiti:            []byte("example"),
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

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shares) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shares[spectypes.ValidatorPK(pubKey)]
		},
	)
	store.handleSharesAdded(maps.Values(shares)...)

	pubKeys := maps.Keys(shares)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		randomShares := make([]*ssvtypes.SSVShare, 500)
		first := rand.Intn(len(pubKeys))
		for j := 0; j < 500; j++ {
			randomShares[j] = shares[pubKeys[(first+j)%len(pubKeys)]]
		}

		store.handleSharesUpdated(randomShares...)
	}
}

// Test for error handling and nil states
func TestValidatorStore_HandleNilAndEmptyStates(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	// Attempt to remove a non-existing share
	t.Run("remove non-existing share", func(t *testing.T) {
		store.handleShareRemoved(spectypes.ValidatorPK{99, 88, 77})
		// Ensure store remains unaffected
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})

	// Add nil share - this should be a no-op or handled gracefully
	t.Run("add nil share", func(t *testing.T) {
		require.NotPanics(t, func() {
			store.handleSharesAdded(nil)
		})
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})
}

func TestValidatorStore_EmptyStoreOperations(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	// Correctly sized pubKey array for testing
	var pubKey spectypes.ValidatorPK
	copy(pubKey[:], []byte{1, 2, 3}) // Populate with test data; remaining bytes are zeroed

	t.Run("validate empty store operations", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
		require.Nil(t, store.Validator(pubKey[:]))
		require.Nil(t, store.Committee(spectypes.CommitteeID{1}))
		require.Len(t, store.OperatorValidators(1), 0)
		require.Len(t, store.OperatorCommittees(1), 0)
	})
}

func TestValidatorStore_AddDuplicateShares(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	shareMap[share1.ValidatorPubKey] = share1
	store.handleSharesAdded(share1)

	t.Run("validate store after adding duplicate shares", func(t *testing.T) {
		store.handleSharesAdded(share1) // Add duplicate
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), share1)
	})
}

func TestValidatorStore_UpdateNonExistingShare(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	t.Run("update non-existing share", func(t *testing.T) {
		require.NotPanics(t, func() {
			store.handleSharesUpdated(share1) // Update without adding
		})
		require.Len(t, store.Validators(), 0)
		require.Nil(t, store.Validator(share1.ValidatorPubKey[:]))
	})
}

func TestValidatorStore_RemoveNonExistingShare(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	t.Run("remove non-existing share", func(t *testing.T) {
		store.handleShareRemoved(share1.ValidatorPubKey) // Remove without adding
		require.Len(t, store.Validators(), 0)
		require.Nil(t, store.Validator(share1.ValidatorPubKey[:]))
	})
}

func TestValidatorStore_UpdateNilData(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	// Add a valid share and simulate a nil entry in byOperatorID
	shareMap[share1.ValidatorPubKey] = share1
	store.handleSharesAdded(share1)

	// Manually set a nil entry for a signer in byOperatorID
	store.muOperatorID.Lock()
	store.byOperatorID[share1.Committee[0].Signer] = nil
	store.muOperatorID.Unlock()

	t.Run("update with nil data in byOperatorID", func(t *testing.T) {
		require.NotPanics(t, func() {
			store.handleSharesUpdated(share1) // Attempt to update share1
		})

		// Validate that the state remains consistent and does not crash
		require.Len(t, store.Validators(), 1)
		require.Equal(t, share1, store.Validator(share1.ValidatorPubKey[:]))
	})
}

func TestValidatorStore_HandlingDifferentStatuses(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	share3 := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(3),
			ValidatorPubKey:     spectypes.ValidatorPK{10, 11, 12},
			SharePubKey:         spectypes.ShareValidatorPK{13, 14, 15},
			Committee:           []*spectypes.ShareMember{{Signer: 3}},
			FeeRecipientAddress: [20]byte{70, 80, 90},
			Graffiti:            []byte("status_test"),
		},
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Index:           phase0.ValidatorIndex(3),
				ActivationEpoch: 300,
				Status:          eth2apiv1.ValidatorStateActiveOngoing,
			},
			OwnerAddress: common.HexToAddress("0xabcde"),
			Liquidated:   false,
		},
	}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	shareMap[share3.ValidatorPubKey] = share3
	store.handleSharesAdded(share3)

	t.Run("check shares with different statuses", func(t *testing.T) {
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), share3)

		require.Len(t, store.ParticipatingValidators(301), 1) // Active status should be participating
		require.Equal(t, share3, store.ParticipatingValidators(301)[0])
	})
}

func TestValidatorStore_AddRemoveBulkShares(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	// Create a large number of shares
	var bulkShares []*ssvtypes.SSVShare
	for i := 0; i < 100; i++ {
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorIndex:      phase0.ValidatorIndex(i),
				ValidatorPubKey:     spectypes.ValidatorPK{byte(i), byte(i + 1), byte(i + 2)},
				SharePubKey:         spectypes.ShareValidatorPK{byte(i + 3), byte(i + 4), byte(i + 5)},
				Committee:           []*spectypes.ShareMember{{Signer: spectypes.OperatorID(i % 5)}},
				FeeRecipientAddress: [20]byte{byte(i)},
				Graffiti:            []byte("bulk_add"),
			},
			Metadata: ssvtypes.Metadata{
				BeaconMetadata: &beaconprotocol.ValidatorMetadata{
					Index:           phase0.ValidatorIndex(i),
					ActivationEpoch: phase0.Epoch(i),
					Status:          eth2apiv1.ValidatorStatePendingQueued,
				},
				OwnerAddress: common.HexToAddress(fmt.Sprintf("0x%x", i)),
				Liquidated:   false,
			},
		}
		bulkShares = append(bulkShares, share)
		shareMap[share.ValidatorPubKey] = share
	}

	store.handleSharesAdded(bulkShares...)

	t.Run("check bulk added shares", func(t *testing.T) {
		require.Len(t, store.Validators(), 100)
		for _, share := range bulkShares {
			require.Contains(t, store.Validators(), share)
		}
	})

	// Remove all shares
	for _, share := range bulkShares {
		store.handleShareRemoved(share.ValidatorPubKey)
		delete(shareMap, share.ValidatorPubKey)
	}

	t.Run("check state after bulk removal", func(t *testing.T) {
		require.Len(t, store.Validators(), 0)
		require.Len(t, store.Committees(), 0)
	})
}

func TestValidatorStore_MixedOperations(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	// Initial adds
	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	store.handleSharesAdded(share1, share2)

	// Mixed operations
	store.handleShareRemoved(share1.ValidatorPubKey)
	shareMap[share1.ValidatorPubKey] = share1 // Re-add share1
	store.handleSharesAdded(share1)
	shareMap[updatedShare2.ValidatorPubKey] = updatedShare2
	store.handleSharesUpdated(updatedShare2) // Update share2

	t.Run("check mixed operations result", func(t *testing.T) {
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), updatedShare2)
	})
}

func TestValidatorStore_InvalidCommitteeHandling(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	invalidCommitteeShare := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			ValidatorIndex:      phase0.ValidatorIndex(10),
			ValidatorPubKey:     spectypes.ValidatorPK{10, 20, 30},
			SharePubKey:         spectypes.ShareValidatorPK{40, 50, 60},
			Committee:           []*spectypes.ShareMember{{Signer: 1}, {Signer: 1}}, // Duplicate members
			FeeRecipientAddress: [20]byte{70, 80, 90},
			Graffiti:            []byte("invalid_committee"),
		},
		Metadata: ssvtypes.Metadata{
			BeaconMetadata: &beaconprotocol.ValidatorMetadata{
				Index:           phase0.ValidatorIndex(10),
				ActivationEpoch: 500,
				Status:          eth2apiv1.ValidatorStatePendingQueued,
			},
			OwnerAddress: common.HexToAddress("0xdeadbeef"),
			Liquidated:   false,
		},
	}

	shareMap[invalidCommitteeShare.ValidatorPubKey] = invalidCommitteeShare
	store.handleSharesAdded(invalidCommitteeShare)

	t.Run("check invalid committee handling", func(t *testing.T) {
		require.Len(t, store.Validators(), 1)
		require.Contains(t, store.Validators(), invalidCommitteeShare)
		committee := store.Committee(invalidCommitteeShare.CommitteeID())
		require.NotNil(t, committee)
		require.Len(t, committee.Operators, 1) // Should handle duplicates
	})
}

func TestValidatorStore_HighContentionConcurrency(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}
	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2
	store.handleSharesAdded(share1, share2)

	var wg sync.WaitGroup
	wg.Add(100)

	// High contention test with concurrent read, add, update, and remove
	for i := 0; i < 25; i++ {
		go func() {
			defer wg.Done()
			store.handleSharesAdded(share1, share2)
		}()
		go func() {
			defer wg.Done()
			store.handleSharesUpdated(updatedShare2)
		}()
		go func() {
			defer wg.Done()
			store.handleShareRemoved(share1.ValidatorPubKey)
		}()
		go func() {
			defer wg.Done()
			_ = store.Validator(share1.ValidatorPubKey[:])
			_ = store.Committee(share1.CommitteeID())
			_ = store.Validators()
			_ = store.Committees()
		}()
	}

	wg.Wait()

	t.Run("validate high contention state", func(t *testing.T) {
		// Check that the store is consistent and valid after high contention
		require.NotPanics(t, func() {
			store.Validators()
			store.Committees()
			store.OperatorValidators(1)
			store.OperatorCommittees(1)
		})
	})
}

func TestValidatorStore_BulkAddUpdate(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

	// Initial shares to add
	shareMap[share1.ValidatorPubKey] = share1
	shareMap[share2.ValidatorPubKey] = share2

	t.Run("bulk add shares", func(t *testing.T) {
		store.handleSharesAdded(share1, share2)
		require.Len(t, store.Validators(), 2)
		require.Contains(t, store.Validators(), share1)
		require.Contains(t, store.Validators(), share2)
	})

	// Update shares
	share1.Metadata.BeaconMetadata.Status = eth2apiv1.ValidatorStateActiveOngoing
	share2.Metadata.BeaconMetadata.Status = eth2apiv1.ValidatorStateActiveOngoing

	t.Run("bulk update shares", func(t *testing.T) {
		store.handleSharesUpdated(share1, share2)
		require.Equal(t, eth2apiv1.ValidatorStateActiveOngoing, store.Validator(share1.ValidatorPubKey[:]).Metadata.BeaconMetadata.Status)
	})
}

func TestValidatorStore_ComprehensiveIndex(t *testing.T) {
	shareMap := map[spectypes.ValidatorPK]*ssvtypes.SSVShare{}

	store := newValidatorStore(
		func() []*ssvtypes.SSVShare { return maps.Values(shareMap) },
		func(pubKey []byte) *ssvtypes.SSVShare {
			return shareMap[spectypes.ValidatorPK(pubKey)]
		},
	)

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
		store.handleSharesAdded(noMetadataShare)

		require.Nil(t, store.ValidatorByIndex(0))
		require.Len(t, store.OperatorValidators(3), 1)
		require.Equal(t, noMetadataShare, store.OperatorValidators(3)[0])
	})

	// Update share to have metadata
	noMetadataShare.Metadata = ssvtypes.Metadata{
		BeaconMetadata: &beaconprotocol.ValidatorMetadata{
			Index: phase0.ValidatorIndex(10),
		},
	}

	t.Run("update share with metadata", func(t *testing.T) {
		store.handleSharesUpdated(noMetadataShare)

		require.Equal(t, noMetadataShare, store.ValidatorByIndex(10))
	})

	t.Run("remove share", func(t *testing.T) {
		store.handleShareRemoved(noMetadataShare.ValidatorPubKey)
		// Remove from shareMap to mimic actual behavior
		delete(shareMap, noMetadataShare.ValidatorPubKey)

		// Ensure complete removal
		require.Nil(t, store.ValidatorByIndex(10), "Validator by index should be nil after removal")
		require.Empty(t, store.OperatorValidators(3), "Operator validators should be empty after removal")
		require.Nil(t, store.Validator(noMetadataShare.ValidatorPubKey[:]), "Validator should be nil after removal")
	})
}
