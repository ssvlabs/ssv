package networkconfig

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestSSVForkName_String(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		fork     SSVForkName
		expected string
	}{
		{
			name:     "alan fork",
			fork:     Alan,
			expected: "Alan",
		},
		{
			name:     "finality consensus fork",
			fork:     FinalityConsensus,
			expected: "Finality Consensus",
		},
		{
			name:     "unknown fork",
			fork:     SSVForkName(999),
			expected: "Unknown fork",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.expected, tt.fork.String())
		})
	}
}

func TestSSVForks_ActiveFork(t *testing.T) {
	t.Parallel()

	forks := SSVForks{
		{Name: "Fork1", Epoch: 100},
		{Name: "Fork2", Epoch: 200},
	}

	testCases := []struct {
		name          string
		epoch         phase0.Epoch
		expectedFork  string
		expectNilFork bool
	}{
		{
			name:          "before first fork",
			epoch:         50,
			expectNilFork: true,
		},
		{
			name:         "at first fork",
			epoch:        100,
			expectedFork: "Fork1",
		},
		{
			name:         "between first and second fork",
			epoch:        150,
			expectedFork: "Fork1",
		},
		{
			name:         "at second fork",
			epoch:        200,
			expectedFork: "Fork2",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := forks.ActiveFork(tt.epoch)
			if tt.expectNilFork {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.expectedFork, result.Name)
			}
		})
	}
}

func TestSSVForks_ComputeActiveFork(t *testing.T) {
	t.Parallel()

	forks := SSVForks{
		{Name: "Fork1", Epoch: 100},
		{Name: "Fork2", Epoch: 200},
	}

	testCases := []struct {
		name          string
		epoch         phase0.Epoch
		expectedFork  string
		expectNilFork bool
	}{
		{
			name:          "before first fork",
			epoch:         50,
			expectNilFork: true,
		},
		{
			name:         "at first fork",
			epoch:        100,
			expectedFork: "Fork1",
		},
		{
			name:         "between first and second fork",
			epoch:        150,
			expectedFork: "Fork1",
		},
		{
			name:         "at second fork",
			epoch:        200,
			expectedFork: "Fork2",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := forks.ComputeActiveFork(tt.epoch)
			if tt.expectNilFork {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.expectedFork, result.Name)
			}
		})
	}
}

func TestSSVForks_FindByName(t *testing.T) {
	t.Parallel()

	forks := SSVForks{
		{Name: "Fork1", Epoch: 100},
		{Name: "Fork2", Epoch: 200},
	}

	testCases := []struct {
		name          string
		forkName      string
		expectedEpoch phase0.Epoch
		expectNilFork bool
	}{
		{
			name:          "existing fork",
			forkName:      "Fork2",
			expectedEpoch: 200,
		},
		{
			name:          "non-existent fork",
			forkName:      "ForkX",
			expectNilFork: true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := forks.FindByName(tt.forkName)
			if tt.expectNilFork {
				require.Nil(t, result)
			} else {
				require.NotNil(t, result)
				require.Equal(t, tt.forkName, result.Name)
				require.Equal(t, tt.expectedEpoch, result.Epoch)
			}
		})
	}
}

func TestSSVForks_IsForkActive(t *testing.T) {
	t.Parallel()

	forks := SSVForks{
		{Name: "Fork1", Epoch: 100},
		{Name: "Fork2", Epoch: 200},
	}

	testCases := []struct {
		name         string
		forkName     string
		epoch        phase0.Epoch
		expectActive bool
	}{
		{
			name:         "before first fork activation",
			forkName:     "Fork1",
			epoch:        50,
			expectActive: false,
		},
		{
			name:         "at fork activation",
			forkName:     "Fork1",
			epoch:        100,
			expectActive: true,
		},
		{
			name:         "after fork activation but before next fork",
			forkName:     "Fork1",
			epoch:        150,
			expectActive: true,
		},
		{
			name:         "after next fork activation",
			forkName:     "Fork1",
			epoch:        200,
			expectActive: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := forks.IsForkActive(tt.forkName, tt.epoch)
			require.Equal(t, tt.expectActive, result)
		})
	}
}

func TestSSVForkConfig_Delegation(t *testing.T) {
	t.Parallel()

	forks := SSVForks{
		{Name: "Fork1", Epoch: 100},
		{Name: "Fork2", Epoch: 200},
		{Name: "Finality Consensus", Epoch: 300},
	}

	config := SSVForkConfig{Forks: forks}

	t.Run("ActiveFork", func(t *testing.T) {
		t.Parallel()

		result := config.ActiveFork(250)
		require.NotNil(t, result)
		require.Equal(t, "Fork2", result.Name)
	})

	t.Run("FindForkByName", func(t *testing.T) {
		t.Parallel()

		result := config.FindForkByName("Fork1")
		require.NotNil(t, result)
		require.Equal(t, phase0.Epoch(100), result.Epoch)
	})

	t.Run("IsForkActive", func(t *testing.T) {
		t.Parallel()

		result := config.IsForkActive("Fork2", 250)
		require.True(t, result)
	})

	t.Run("GetFinalityConsensusEpoch", func(t *testing.T) {
		t.Parallel()

		result := config.GetFinalityConsensusEpoch()
		require.Equal(t, phase0.Epoch(300), result)
	})

	t.Run("GetFinalityConsensusEpoch not found", func(t *testing.T) {
		t.Parallel()

		emptyConfig := SSVForkConfig{Forks: SSVForks{}}
		result := emptyConfig.GetFinalityConsensusEpoch()
		require.Equal(t, MaxEpoch, result)
	})
}
