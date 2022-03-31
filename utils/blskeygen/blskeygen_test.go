package blskeygen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenBLSKeyPair(t *testing.T) {
	sk, pk := GenBLSKeyPair()
	require.NotNil(t, sk)
	require.NotNil(t, pk)
}
