package discovery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// isSubnet checks if the given string is a subnet string
func isSubnet(ns string) bool {
	r, done := regPool.Get()
	defer done()
	return r.MatchString(ns)
}

func TestNsToSubnet(t *testing.T) {
	tests := []struct {
		name        string
		ns          string
		expected    uint64
		expectedErr string
		isSubnet    bool
	}{
		{
			"dummy string",
			"xxx",
			0,
			errPatternMismatch.Error(),
			false,
		},
		{
			"invalid int",
			"floodsub:bloxstaking.ssv.xxx",
			0,
			errPatternMismatch.Error(),
			false,
		},
		{
			"invalid pattern",
			"floodsub:ssv.1",
			0,
			errPatternMismatch.Error(),
			false,
		},
		{
			"uint overflow",
			"floodsub:bloxstaking.ssv.99999999999999999999",
			0,
			`strconv.ParseUint: parsing "99999999999999999999": value out of range`,
			true,
		},
		{
			"value out of range",
			"floodsub:bloxstaking.ssv.128",
			0,
			errValueOutOfRange.Error(),
			true,
		},
		{
			"valid",
			"floodsub:bloxstaking.ssv.127",
			127,
			"",
			true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.isSubnet, isSubnet(test.ns))

			dvs := &DiscV5Service{}

			subnet, err := dvs.nsToSubnet(test.ns)
			if test.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.expectedErr)
			}
			require.Equal(t, test.expected, subnet)
		})
	}
}
