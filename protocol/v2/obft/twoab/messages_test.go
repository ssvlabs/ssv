package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValueRoot_SameVProducesSameRoot(t *testing.T) {
	v := Value("abcdef")
	require.Equal(t, ValueRoot(v), ValueRoot(append(Value{}, v...)))
}

func TestValueRoot_DifferentVProducesDifferentRoot(t *testing.T) {
	require.NotEqual(t, ValueRoot(Value("abc")), ValueRoot(Value("abcd")))
}

func TestValueRoot_EmptyV(t *testing.T) {
	require.NotEqual(t, [32]byte{}, ValueRoot(Value("")))
}

func TestLayerEntryKind_String(t *testing.T) {
	require.Equal(t, "empty", LayerEntryEmpty.String())
	require.Equal(t, "sigma-chained", LayerEntrySigmaChained.String())
	require.Equal(t, "nr-plaintext", LayerEntryNRPlaintext.String())
	require.Equal(t, "unspecified", LayerEntryKind(0xff).String())
}

func TestCommitSide_String(t *testing.T) {
	require.Equal(t, "nr", CommitSideNR.String())
	require.Equal(t, "nr-direct", CommitSideNRDirect.String())
	require.Equal(t, "unspecified", CommitSideUnspecified.String())
	// 0x01 is not a valid Commit side — String() returns "unspecified" for
	// any unrecognized value.
	require.Equal(t, "unspecified", CommitSide(0x01).String())
}

func TestCommitSide_IsNR(t *testing.T) {
	require.True(t, CommitSideNR.IsNR())
	require.True(t, CommitSideNRDirect.IsNR())
	require.False(t, CommitSideUnspecified.IsNR())
}

func TestValueMsgContentHash_IdenticalProducesSameHash(t *testing.T) {
	vm := &ValueMsg{
		ClusterID:  [32]byte{1, 2, 3},
		OperatorID: 1,
		Height:     1,
		V:          Value("V0"),
		ValueRoot:  ValueRoot(Value("V0")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntrySigmaChained, V: Value("V1"), Payload: []byte("p1")},
		},
	}
	require.Equal(t, valueMsgContentHash(vm), valueMsgContentHash(vm))
}

func TestValueMsgContentHash_DistinguishesByV(t *testing.T) {
	vmA := &ValueMsg{ClusterID: [32]byte{1}, OperatorID: 1, Height: 1, V: Value("A"), ValueRoot: ValueRoot(Value("A"))}
	vmB := &ValueMsg{ClusterID: [32]byte{1}, OperatorID: 1, Height: 1, V: Value("B"), ValueRoot: ValueRoot(Value("B"))}
	require.NotEqual(t, valueMsgContentHash(vmA), valueMsgContentHash(vmB))
}

func TestNoValueMsgContentHash_IdenticalProducesSameHash(t *testing.T) {
	nv := &NoValueMsg{
		ClusterID:  [32]byte{1, 2, 3},
		OperatorID: 1,
		Height:     1,
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: []byte("p1")},
		},
	}
	require.Equal(t, noValueMsgContentHash(nv), noValueMsgContentHash(nv))
}

func TestCommitContentHash_DistinguishesBySide(t *testing.T) {
	// Only NR and NRDirect are valid sides. The two carry different
	// LayerEntries shapes (NR is empty; NRDirect has K-1 entries), so
	// distinct hashes regardless of partial bytes.
	cA := &Commit{ClusterID: [32]byte{1}, OperatorID: 1, Height: 1, Side: CommitSideNR, L0Partial: Signature("p")}
	cB := &Commit{ClusterID: [32]byte{1}, OperatorID: 1, Height: 1, Side: CommitSideNRDirect, L0Partial: Signature("p")}
	require.NotEqual(t, commitContentHash(cA), commitContentHash(cB))
}

func TestCommitContentHash_DistinguishesByL0Partial(t *testing.T) {
	// Distinct commits differ on L0Partial bytes (e.g., distinct NR
	// partials from the same op across re-broadcast — content-hash must
	// capture this for dedup).
	cA := &Commit{ClusterID: [32]byte{1}, OperatorID: 1, Height: 1, Side: CommitSideNR, L0Partial: Signature("pA")}
	cB := &Commit{ClusterID: [32]byte{1}, OperatorID: 1, Height: 1, Side: CommitSideNR, L0Partial: Signature("pB")}
	require.NotEqual(t, commitContentHash(cA), commitContentHash(cB))
}
