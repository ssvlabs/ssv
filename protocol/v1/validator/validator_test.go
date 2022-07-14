package validator

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"testing"

	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zapcore.DebugLevel, nil)
}

func TestIdentifierTest(t *testing.T) {
	node := testingValidator(t, true, 4, []byte{1, 2, 3, 4})
	require.True(t, oneOfIBFTIdentifiers(node, []byte{1, 2, 3, 4}))
	require.False(t, oneOfIBFTIdentifiers(node, []byte{1, 2, 3, 3}))
}

func oneOfIBFTIdentifiers(v *Validator, toMatch []byte) bool {
	for _, i := range v.ibfts {
		if bytes.Equal(i.GetIdentifier(), toMatch) {
			return true
		}
	}
	return false
}
