package ssv

import (
	"fmt"
	"github.com/bloxapp/ssv/utils/logex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"testing"
)

func TestConvertDutyRunner(t *testing.T) {
	logex.Build("test", zapcore.DebugLevel, nil)

	runner := State{}
	require.NoError(t, runner.Decode([]byte("0351bb303531bc5858d312928c9577e9ca0104f3d8986a34fce30f2519908b1e")))
	fmt.Printf("")
}
