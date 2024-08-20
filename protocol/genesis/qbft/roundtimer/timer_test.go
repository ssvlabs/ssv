package roundtimer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/protocol/genesis/qbft/roundtimer/mocks"
)

func TestTimeoutForRound(t *testing.T) {
	roles := []genesisspectypes.BeaconRole{
		genesisspectypes.BNRoleAttester,
		genesisspectypes.BNRoleAggregator,
		genesisspectypes.BNRoleProposer,
		genesisspectypes.BNRoleSyncCommittee,
		genesisspectypes.BNRoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		t.Run(fmt.Sprintf("TimeoutForRound - %s: <= quickTimeoutThreshold", role), func(t *testing.T) {
			testTimeoutForRound(t, role, genesisspecqbft.Round(1))
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: > quickTimeoutThreshold", role), func(t *testing.T) {
			testTimeoutForRound(t, role, genesisspecqbft.Round(2))
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: before elapsed", role), func(t *testing.T) {
			testTimeoutForRoundElapsed(t, role, genesisspecqbft.Round(2))
		})

		// TODO: Decide if to make the proposer timeout deterministic
		// Proposer role is not tested for multiple synchronized timers since it's not deterministic
		if role == genesisspectypes.BNRoleProposer {
			continue
		}

		t.Run(fmt.Sprintf("TimeoutForRound - %s: multiple synchronized timers", role), func(t *testing.T) {
			testTimeoutForRoundMulti(t, role, genesisspecqbft.Round(1))
		})
	}
}

func setupMockBeaconNetwork(t *testing.T) *mocks.MockBeaconNetwork {
	ctrl := gomock.NewController(t)
	mockBeaconNetwork := mocks.NewMockBeaconNetwork(ctrl)

	mockBeaconNetwork.EXPECT().SlotDurationSec().Return(120 * time.Millisecond).AnyTimes()
	mockBeaconNetwork.EXPECT().GetSlotStartTime(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) time.Time {
			return time.Now()
		},
	).AnyTimes()
	return mockBeaconNetwork
}

func setupTimer(mockBeaconNetwork *mocks.MockBeaconNetwork, onTimeout OnRoundTimeoutF, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) *RoundTimer {
	timer := New(context.Background(), mockBeaconNetwork, role, onTimeout)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: round,
		quick:          100 * time.Millisecond,
		slow:           200 * time.Millisecond,
	}

	return timer
}

func testTimeoutForRound(t *testing.T, role genesisspectypes.BeaconRole, threshold genesisspecqbft.Round) {
	mockBeaconNetwork := setupMockBeaconNetwork(t)

	count := int32(0)
	onTimeout := func(round genesisspecqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(mockBeaconNetwork, onTimeout, role, threshold)

	timer.TimeoutForRound(genesisspecqbft.FirstHeight, threshold)
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, threshold) + time.Millisecond*10)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundElapsed(t *testing.T, role genesisspectypes.BeaconRole, threshold genesisspecqbft.Round) {
	mockBeaconNetwork := setupMockBeaconNetwork(t)

	count := int32(0)
	onTimeout := func(round genesisspecqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(mockBeaconNetwork, onTimeout, role, threshold)

	timer.TimeoutForRound(genesisspecqbft.FirstHeight, genesisspecqbft.FirstRound)
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, genesisspecqbft.FirstRound) / 2)
	timer.TimeoutForRound(genesisspecqbft.FirstHeight, genesisspecqbft.Round(2)) // reset before elapsed
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, genesisspecqbft.Round(2)) + time.Millisecond*10)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundMulti(t *testing.T, role genesisspectypes.BeaconRole, threshold genesisspecqbft.Round) {
	ctrl := gomock.NewController(t)
	mockBeaconNetwork := mocks.NewMockBeaconNetwork(ctrl)

	var count int32
	var timestamps = make([]int64, 4)
	var mu sync.Mutex

	onTimeout := func(index int) {
		atomic.AddInt32(&count, 1)
		mu.Lock()
		timestamps[index] = time.Now().UnixNano()
		mu.Unlock()
	}

	timeNow := time.Now()
	mockBeaconNetwork.EXPECT().SlotDurationSec().Return(100 * time.Millisecond).AnyTimes()
	mockBeaconNetwork.EXPECT().GetSlotStartTime(gomock.Any()).DoAndReturn(
		func(slot phase0.Slot) time.Time {
			return timeNow
		},
	).AnyTimes()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(index int) {
			timer := New(context.Background(), mockBeaconNetwork, role, func(round genesisspecqbft.Round) { onTimeout(index) })
			timer.timeoutOptions = TimeoutOptions{
				quickThreshold: threshold,
				quick:          100 * time.Millisecond,
			}
			timer.TimeoutForRound(genesisspecqbft.FirstHeight, genesisspecqbft.FirstRound)
			wg.Done()
		}(i)
		time.Sleep(time.Millisecond * 10) // Introduce a sleep between creating timers
	}

	wg.Wait() // Wait for all go-routines to finish

	timer := New(context.Background(), mockBeaconNetwork, role, nil)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: genesisspecqbft.Round(1),
		quick:          100 * time.Millisecond,
	}

	// Wait a bit more than the expected timeout to ensure all timers have triggered
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, genesisspecqbft.FirstRound) + time.Millisecond*100)

	require.Equal(t, int32(4), atomic.LoadInt32(&count), "All four timers should have triggered")

	mu.Lock()
	for i := 1; i < 4; i++ {
		require.InDelta(t, timestamps[0], timestamps[i], float64(time.Millisecond*10), "All four timers should expire nearly at the same time")
	}
	mu.Unlock()
}
