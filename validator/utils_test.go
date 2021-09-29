package validator

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdentifierUnformat(t *testing.T) {
	pk, role := IdentifierUnformat("xxx_ATTESTER")
	require.Equal(t, "xxx", pk)
	require.Equal(t, "ATTESTER", role)
}

func TestIdentifierFormat(t *testing.T) {
	identifier := IdentifierFormat([]byte("xxx"), beacon.RoleTypeAttester)
	require.Equal(t, "787878_ATTESTER", identifier)
}
