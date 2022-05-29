package conversion

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdentifierConversion(t *testing.T) {
	v0Identifier := "YTAyZjNhNGQ5ZDg2NTZkMmI0NDI3NzVjM2JlNDliMzU1ZDc0MDU5OGNiYjM5NzAyNmZhNzRkYzUxZTFlN2FhOGVmZTJjZjk3ZTQ0ZjZmYWQxMTE4NWY2M2I2MTUxY2Q0X0FUVEVTVEVS"
	old, err := base64.StdEncoding.DecodeString(v0Identifier)
	require.NoError(t, err)
	n := toIdentifierV1(old)
	o := toIdentifierV0(n)

	pk, err := hex.DecodeString("a02f3a4d9d8656d2b442775c3be49b355d740598cbb397026fa74dc51e1e7aa8efe2cf97e44f6fad11185f63b6151cd4")
	require.NoError(t, err)
	nExpeceted := message.NewIdentifier(pk, message.RoleTypeAttester)

	require.Equal(t, string(old), string(o))
	require.True(t, bytes.Equal(n, nExpeceted))
}
