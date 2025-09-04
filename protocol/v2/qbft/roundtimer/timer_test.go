package roundtimer

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	slotDuration = 600 * time.Millisecond

	quickTimeout = 300 * time.Millisecond
	slowTimeout  = 600 * time.Millisecond

	// safeTestDelay is a safety buffer (a guesstimate) to use in tests that set expectations making certain
	// assumptions regarding the go-routine scheduling.
	safeTestDelay = 100 * time.Millisecond
)

func TestTimeoutForRound(t *testing.T) {
	roles := []spectypes.RunnerRole{
		spectypes.RoleCommittee,
		spectypes.RoleAggregator,
		spectypes.RoleProposer,
		spectypes.RoleSyncCommitteeContribution,
	}

	for _, role := range roles {
		t.Run(fmt.Sprintf("TimeoutForRound - %s: <= quickTimeoutThreshold", role), func(t *testing.T) {
			testTimeoutForRound(t, role, specqbft.Round(1))
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: > quickTimeoutThreshold", role), func(t *testing.T) {
			testTimeoutForRound(t, role, specqbft.Round(2))
		})

		t.Run(fmt.Sprintf("TimeoutForRound - %s: before elapsed", role), func(t *testing.T) {
			testTimeoutForRoundElapsed(t, role, specqbft.Round(2))
		})

		// TODO: Decide if to make the proposer timeout deterministic
		// Proposer role is not tested for multiple synchronized timers since it's not deterministic
		if role == spectypes.RoleProposer {
			continue
		}

		t.Run(fmt.Sprintf("TimeoutForRound - %s: multiple synchronized timers", role), func(t *testing.T) {
			testTimeoutForRoundMulti(t, role, specqbft.Round(1))
		})
	}
}

func setupTestBeaconConfig() *networkconfig.Beacon {
	config := *networkconfig.TestNetwork.Beacon
	config.SlotDuration = slotDuration
	config.GenesisTime = time.Now()

	return &config
}

func setupTimer(
	t *testing.T,
	beaconConfig *networkconfig.Beacon,
	onTimeout OnRoundTimeoutF,
	role spectypes.RunnerRole,
	round specqbft.Round,
) *RoundTimer {
	timer := New(t.Context(), beaconConfig, role, onTimeout)
	timer.timeoutOptions = TimeoutOptions{
		quickThreshold: round,
		quick:          quickTimeout,
		slow:           slowTimeout,
	}

	return timer
}

func testTimeoutForRound(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(t, testBeaconConfig, onTimeout, role, threshold)

	require.Equal(t, int32(0), atomic.LoadInt32(&count))

	timer.TimeoutForRound(specqbft.FirstHeight, threshold)
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, threshold) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundElapsed(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	count := int32(0)
	onTimeout := func(round specqbft.Round) {
		atomic.AddInt32(&count, 1)
	}

	timer := setupTimer(t, testBeaconConfig, onTimeout, role, threshold)

	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) / 2)
	timer.TimeoutForRound(specqbft.FirstHeight, specqbft.Round(2)) // reset before elapsed
	require.Equal(t, int32(0), atomic.LoadInt32(&count))
	<-time.After(timer.RoundTimeout(specqbft.FirstHeight, specqbft.Round(2)) + safeTestDelay)
	require.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func testTimeoutForRoundMulti(t *testing.T, role spectypes.RunnerRole, threshold specqbft.Round) {
	testBeaconConfig := setupTestBeaconConfig()

	var count int32
	var timestamps = make([]int64, 4)
	var mu sync.Mutex

	onTimeout := func(index int) {
		atomic.AddInt32(&count, 1)
		mu.Lock()
		timestamps[index] = time.Now().UnixNano()
		mu.Unlock()
	}

	for i := 0; i < 4; i++ {
		go func(index int) {
			timer := New(t.Context(), testBeaconConfig, role, func(round specqbft.Round) { onTimeout(index) })
			timer.timeoutOptions = TimeoutOptions{
				quickThreshold: threshold,
				quick:          quickTimeout,
			}
			timer.TimeoutForRound(specqbft.FirstHeight, specqbft.FirstRound)
		}(i)
		// Introduce a sleep between creating timers to simulate a real-world scenario.
		time.Sleep(time.Millisecond * 10)
	}

	// To set up the correct expectations, we need to know when exactly this particular `role` is supposed to
	// timeout (different roles time out at different times into slot). We need to use a reference-timer for that.
	referenceTimer := New(t.Context(), testBeaconConfig, role, nil)
	referenceTimer.timeoutOptions = TimeoutOptions{
		quickThreshold: specqbft.Round(1),
		quick:          quickTimeout,
	}
	expectedTimeout := referenceTimer.RoundTimeout(specqbft.FirstHeight, specqbft.FirstRound) + quickTimeout
	<-time.After(expectedTimeout + safeTestDelay)

	require.Equal(t, int32(4), atomic.LoadInt32(&count), "All four timers should have triggered")
	mu.Lock()
	for i := 1; i < 4; i++ {
		require.InDelta(t, timestamps[0], timestamps[i], float64(safeTestDelay), "All four timers should expire nearly at the same time")
	}
	mu.Unlock()
}
