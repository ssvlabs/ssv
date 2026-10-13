package operator

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateDocCmd is a smoke test: the doc command builds a defaulted config and renders the
// documentation table (via cli/config.Describe) without panicking. Output is discarded.
func TestGenerateDocCmd(t *testing.T) {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	defer devnull.Close()

	orig := os.Stdout
	os.Stdout = devnull
	defer func() { os.Stdout = orig }()

	require.NotPanics(t, func() { GenerateDocCmd.Run(GenerateDocCmd, nil) })
}
