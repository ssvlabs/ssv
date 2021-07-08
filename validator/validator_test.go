package validator

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdentifierTest(t *testing.T) {
	node := testingValidator(t, true, 4, []byte{1, 2, 3, 4})
	require.True(t, node.oneOfIBFTIdentifiers([]byte{1, 2, 3, 4}))
	require.False(t, node.oneOfIBFTIdentifiers([]byte{1, 2, 3, 3}))
}
