package format

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdentifierUnformat(t *testing.T) {
	pk, role := IdentifierUnformat("xxx_ATTESTER")
	require.Equal(t, "xxx", pk)
	require.Equal(t, "ATTESTER", role)
}

func TestIdentifierFormat(t *testing.T) {
	identifier := IdentifierFormat([]byte("111"), "ATTESTER")
	require.Equal(t, "313131_ATTESTER", identifier)
}
