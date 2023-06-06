package discovery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNsToSubnet(t *testing.T) {
	tests := []struct {
		name        string
		ns          string
		expected    int
		expectedErr string
		isSubnet    bool
	}{
		{
			"dummy string",
			"xxx",
			0,
			errNotFound.Error(),
			false,
		},
		{
			"invalid int",
			"floodsub:bloxstaking.ssv.xxx",
			0,
			errNotFound.Error(),
			false,
		},
		{
			"invalid",
			"floodsub:ssv.1",
			0,
			errNotFound.Error(),
			false,
		},
		{
			"valid",
			"floodsub:bloxstaking.ssv.21",
			21,
			"",
			true,
		},
		{
			"uint overflow",
			"floodsub:bloxstaking.ssv.99999999999999999999",
			0,
			`strconv.ParseUint: parsing "99999999999999999999": value out of range`,
			true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.isSubnet, isSubnet(test.ns))

			subnet, err := nsToSubnet(test.ns)
			if test.expectedErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, test.expectedErr)
			}
			require.Equal(t, test.expected, subnet)
		})
	}
}
