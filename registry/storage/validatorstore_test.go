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
		Liquidated:   false,
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
	store.handleShareUpdated(updatedShare2)

	// TODO: updatedShare2 now only changes Quorum field, which doesn't affect any indices. If handleShareUpdated expects to receive shares where indexes field are updated, this needs to be tested.

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
		require.Len(t, store.ParticipatingValidators(201), 2)
		require.Contains(t, store.ParticipatingValidators(201), share1)
		require.Contains(t, store.ParticipatingValidators(201), updatedShare2)

		require.Empty(t, store.ParticipatingCommittees(99))
		require.Len(t, store.ParticipatingCommittees(101), 1)
		require.Equal(t, buildCommittee([]*ssvtypes.SSVShare{share1}), store.ParticipatingCommittees(101)[0])
		require.Len(t, store.ParticipatingCommittees(201), 2)
		require.Contains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{share1}))
		require.Contains(t, store.ParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{updatedShare2}))

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
		require.Len(t, selfStore.SelfParticipatingValidators(201), 1)
		require.Contains(t, selfStore.SelfParticipatingValidators(201), updatedShare2)

		require.Empty(t, selfStore.SelfParticipatingCommittees(99))
		require.Len(t, selfStore.SelfParticipatingCommittees(201), 1)
		require.Contains(t, selfStore.SelfParticipatingCommittees(201), buildCommittee([]*ssvtypes.SSVShare{updatedShare2}))
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

		store.handleShareUpdated(randomShares...)
	}
}
