package goclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
