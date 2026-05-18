package goclient

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineBlockFetchPath(t *testing.T) {
	tests := []struct {
		name          string
		options       Options
		proposerDelay time.Duration
		wantPath      BlockFetchPath
		wantErr       string // substring match; empty = no error expected
	}{
		{
			name:          "nothing set -> safe (default)",
			options:       Options{},
			proposerDelay: 0,
			wantPath:      BlockFetchPathSafe,
		},
		{
			name:          "only ProposerDelay set -> legacy",
			options:       Options{},
			proposerDelay: 300 * time.Millisecond,
			wantPath:      BlockFetchPathLegacy,
		},
		{
			name:          "only ProposalSoftTimeout set -> legacy",
			options:       Options{ProposalSoftTimeout: 1500 * time.Millisecond},
			proposerDelay: 0,
			wantPath:      BlockFetchPathLegacy,
		},
		{
			name:          "both ProposerDelay and ProposalSoftTimeout set -> legacy",
			options:       Options{ProposalSoftTimeout: 1500 * time.Millisecond},
			proposerDelay: 300 * time.Millisecond,
			wantPath:      BlockFetchPathLegacy,
		},
		{
			name:          "only ProposalSoftDeadline set -> MEV-optimized",
			options:       Options{ProposalSoftDeadline: 1100 * time.Millisecond},
			proposerDelay: 0,
			wantPath:      BlockFetchPathMEVOptimized,
		},
		{
			name:          "ProposerDelay + ProposalSoftDeadline -> error",
			options:       Options{ProposalSoftDeadline: 1100 * time.Millisecond},
			proposerDelay: 300 * time.Millisecond,
			wantErr:       "ProposalSoftDeadline conflicts with legacy",
		},
		{
			name:          "ProposalSoftTimeout + ProposalSoftDeadline -> error",
			options:       Options{ProposalSoftTimeout: 1500 * time.Millisecond, ProposalSoftDeadline: 1100 * time.Millisecond},
			proposerDelay: 0,
			wantErr:       "ProposalSoftDeadline conflicts with legacy",
		},
		{
			name:          "all three set -> error (legacy + deadline still conflicts)",
			options:       Options{ProposalSoftTimeout: 1500 * time.Millisecond, ProposalSoftDeadline: 1100 * time.Millisecond},
			proposerDelay: 300 * time.Millisecond,
			wantErr:       "ProposalSoftDeadline conflicts with legacy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := DetermineBlockFetchPath(tt.options, tt.proposerDelay)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

func TestValidateProposalSoftDeadline(t *testing.T) {
	tests := []struct {
		name    string
		value   time.Duration
		wantErr bool
	}{
		{name: "at minimum (1000ms) -> ok", value: 1000 * time.Millisecond, wantErr: false},
		{name: "below minimum (999ms) -> error", value: 999 * time.Millisecond, wantErr: true},
		{name: "at safe max (1100ms) -> ok (warn handled externally)", value: 1100 * time.Millisecond, wantErr: false},
		{name: "above safe max but below hard max (2500ms) -> ok", value: 2500 * time.Millisecond, wantErr: false},
		{name: "at hard max (3600ms) -> ok", value: 3600 * time.Millisecond, wantErr: false},
		{name: "above hard max (3601ms) -> error", value: 3601 * time.Millisecond, wantErr: true},
		{name: "zero -> error", value: 0, wantErr: true},
		{name: "negative -> error", value: -100 * time.Millisecond, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProposalSoftDeadline(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "out of range")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestNewOptions_PathDefaulting(t *testing.T) {
	t.Run("safe path defaults ProposalSoftDeadline to 1000ms", func(t *testing.T) {
		base := Options{BeaconNodeAddr: "http://localhost:5052"}
		opts, err := NewOptions(base, 0, BlockFetchPathSafe)
		require.NoError(t, err)
		assert.Equal(t, DefaultProposalSoftDeadline, opts.ProposalSoftDeadline)
		assert.Equal(t, BlockFetchPathSafe, opts.BlockFetchPath)
		// ProposalSoftTimeout should not be touched by the safe path.
		assert.Equal(t, time.Duration(0), opts.ProposalSoftTimeout)
	})

	t.Run("safe path keeps operator-set ProposalSoftDeadline", func(t *testing.T) {
		base := Options{
			BeaconNodeAddr:       "http://localhost:5052",
			ProposalSoftDeadline: 1500 * time.Millisecond,
		}
		opts, err := NewOptions(base, 0, BlockFetchPathSafe)
		require.NoError(t, err)
		assert.Equal(t, 1500*time.Millisecond, opts.ProposalSoftDeadline)
	})

	t.Run("legacy path defaults ProposalSoftTimeout to 1800ms", func(t *testing.T) {
		base := Options{BeaconNodeAddr: "http://localhost:5052"}
		opts, err := NewOptions(base, 0, BlockFetchPathLegacy)
		require.NoError(t, err)
		assert.Equal(t, defaultProposalSoftTimeout, opts.ProposalSoftTimeout)
		assert.Equal(t, BlockFetchPathLegacy, opts.BlockFetchPath)
	})

	t.Run("legacy path subtracts proposer delay from ProposalSoftTimeout", func(t *testing.T) {
		base := Options{BeaconNodeAddr: "http://localhost:5052"}
		opts, err := NewOptions(base, 300*time.Millisecond, BlockFetchPathLegacy)
		require.NoError(t, err)
		assert.Equal(t, defaultProposalSoftTimeout-300*time.Millisecond, opts.ProposalSoftTimeout)
	})

	t.Run("legacy path floors ProposalSoftTimeout at 500ms", func(t *testing.T) {
		// With ProposerDelay = 1500ms, the natural ProposalSoftTimeout would be
		// 1800ms - 1500ms = 300ms, which is below the 500ms floor.
		base := Options{BeaconNodeAddr: "http://localhost:5052"}
		opts, err := NewOptions(base, 1500*time.Millisecond, BlockFetchPathLegacy)
		require.NoError(t, err)
		assert.Equal(t, minProposalSoftTimeout, opts.ProposalSoftTimeout)
	})

	t.Run("legacy path keeps operator-set ProposalSoftTimeout (no reduction)", func(t *testing.T) {
		// When the operator explicitly sets ProposalSoftTimeout, the legacy path
		// uses it as-is without subtracting ProposerDelay (power-user mode).
		base := Options{
			BeaconNodeAddr:      "http://localhost:5052",
			ProposalSoftTimeout: 1200 * time.Millisecond,
		}
		opts, err := NewOptions(base, 300*time.Millisecond, BlockFetchPathLegacy)
		require.NoError(t, err)
		assert.Equal(t, 1200*time.Millisecond, opts.ProposalSoftTimeout)
	})

	t.Run("MEV-optimized path keeps operator-set ProposalSoftDeadline", func(t *testing.T) {
		base := Options{
			BeaconNodeAddr:       "http://localhost:5052",
			ProposalSoftDeadline: 1850 * time.Millisecond,
		}
		opts, err := NewOptions(base, 0, BlockFetchPathMEVOptimized)
		require.NoError(t, err)
		assert.Equal(t, 1850*time.Millisecond, opts.ProposalSoftDeadline)
		assert.Equal(t, BlockFetchPathMEVOptimized, opts.BlockFetchPath)
		// ProposalSoftTimeout should not be touched.
		assert.Equal(t, time.Duration(0), opts.ProposalSoftTimeout)
	})
}

func TestBlockFetchPath_String(t *testing.T) {
	tests := []struct {
		path BlockFetchPath
		want string
	}{
		{BlockFetchPathSafe, "safe"},
		{BlockFetchPathLegacy, "legacy"},
		{BlockFetchPathMEVOptimized, "mev-optimized"},
		{BlockFetchPath(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.path.String())
		})
	}
}
