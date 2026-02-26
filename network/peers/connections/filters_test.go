package connections

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/network/records"
)

func TestNetworkIDFilter(t *testing.T) {
	tests := []struct {
		name       string
		allowed    []string
		received   string
		shouldPass bool
	}{
		{
			name:       "single allowed domain match",
			allowed:    []string{"xxx"},
			received:   "xxx",
			shouldPass: true,
		},
		{
			name:       "single allowed domain mismatch",
			allowed:    []string{"xxx"},
			received:   "bbb",
			shouldPass: false,
		},
		{
			name:       "multiple allowed domains match",
			allowed:    []string{"xxx", "yyy"},
			received:   "yyy",
			shouldPass: true,
		},
		{
			name:       "multiple allowed domains mismatch",
			allowed:    []string{"xxx", "yyy"},
			received:   "zzz",
			shouldPass: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NetworkIDFilter(tc.allowed...)
			err := f("", &records.NodeInfo{NetworkID: tc.received})

			if tc.shouldPass {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}
