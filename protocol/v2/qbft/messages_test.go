package qbft

import (
	"testing"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	"github.com/stretchr/testify/require"
)

func TestHashDataRootMatchesSpec(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		{},
		[]byte("qbft-value"),
		make([]byte, 256),
	} {
		specRoot, err := specqbft.HashDataRoot(data)
		require.NoError(t, err)
		require.Equal(t, specRoot, HashDataRoot(data))
	}
}
