package gloas

import (
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
)

// devnet6GloasBlockSSZ is a real on-chain Gloas SignedBeaconBlock (slot 66) captured from lighthouse
// v8.2.0 (ethpandaops glamsterdam-devnet-6). Its ParentExecutionRequests carries the full EIP-8282
// five-list ExecutionRequests (all lists empty on this block).
//
//go:embed testdata/devnet6_gloas_block.ssz
var devnet6GloasBlockSSZ []byte

// TestSignedBeaconBlockMatchesDevnet6Wire guards that the node's SignedBeaconBlock codec byte-round-trips
// a real Glamsterdam CL's wire format — the check that pins the §4 submit. A future wire drift (a new
// request list, a reordered field) fails here instead of only in a devnet run.
func TestSignedBeaconBlockMatchesDevnet6Wire(t *testing.T) {
	var blk SignedBeaconBlock
	require.NoError(t, blk.UnmarshalSSZ(devnet6GloasBlockSSZ), "decode real v8.2.0 Gloas block")
	require.NotNil(t, blk.Message.Body.ParentExecutionRequests, "Gloas block body carries execution requests")

	out, err := blk.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, len(devnet6GloasBlockSSZ), len(out), "re-marshal length must match the CL wire size")
	require.Equal(t, devnet6GloasBlockSSZ, out,
		"node re-marshal must byte-match v8.2.0's wire format — a mismatch means the Gloas types drifted from the CL")
}
