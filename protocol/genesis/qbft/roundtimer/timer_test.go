package roundtimer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/networkconfig"
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

func setupTimer(beaconConfig networkconfig.Beacon, onTimeout OnRoundTimeoutF, role genesisspectypes.BeaconRole, round genesisspecqbft.Round) *RoundTimer {
	timer := New(context.Background(), beaconConfig, role, onTimeout)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: round,
		quick:          100 * time.Millisecond,
		slow:           200 * time.Millisecond,
	}

	return timer
}

func testTimeoutForRound(t *testing.T, role genesisspectypes.BeaconRole, threshold genesisspecqbft.Round) {
	beaconConfig := networkconfig.TestingBeaconConfig
	beaconConfig.SlotDuration = 120 * time.Millisecond
	beaconConfig.Genesis.GenesisTime = time.Now().Add(500 * time.Millisecond)

	count := int32(0)
	onTimeout := func(round genesisspecqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(beaconConfig, onTimeout, role, threshold)

	timer.TimeoutForRound(genesisspecqbft.FirstHeight, threshold)
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, threshold) + time.Millisecond*10)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundElapsed(t *testing.T, role genesisspectypes.BeaconRole, threshold genesisspecqbft.Round) {
	beaconConfig := networkconfig.TestingBeaconConfig
	beaconConfig.SlotDuration = 120 * time.Millisecond
	beaconConfig.Genesis.GenesisTime = time.Now().Add(500 * time.Millisecond)

	count := int32(0)
	onTimeout := func(round genesisspecqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(beaconConfig, onTimeout, role, threshold)

	timer.TimeoutForRound(genesisspecqbft.FirstHeight, genesisspecqbft.FirstRound)
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, genesisspecqbft.FirstRound) / 2)
	timer.TimeoutForRound(genesisspecqbft.FirstHeight, genesisspecqbft.Round(2)) // reset before elapsed
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(genesisspecqbft.FirstHeight, genesisspecqbft.Round(2)) + time.Millisecond*10)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundMulti(t *testing.T, role genesisspectypes.BeaconRole, threshold genesisspecqbft.Round) {
	var count int32
	var timestamps = make([]int64, 4)
	var mu sync.Mutex

	onTimeout := func(index int) {
		atomic.AddInt32(&count, 1)
		mu.Lock()
		timestamps[index] = time.Now().UnixNano()
		mu.Unlock()
	}

	beaconConfig := networkconfig.TestingBeaconConfig
	beaconConfig.SlotDuration = 100 * time.Millisecond
	beaconConfig.Genesis.GenesisTime = time.Now().Add(500 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(index int) {
			timer := New(context.Background(), beaconConfig, role, func(round genesisspecqbft.Round) { onTimeout(index) })
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

	timer := New(context.Background(), beaconConfig, role, nil)
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
