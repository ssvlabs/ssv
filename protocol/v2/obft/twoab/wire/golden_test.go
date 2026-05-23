package wire

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// Frozen encodings of the fixtures below — one per 2abOBFT envelope kind. A
// diff in any of these means the on-wire layout moved; that is a cross-version
// compatibility break and MUST be a deliberate version-constant bump, not a
// silent refresh of these strings.
//
// twoab is not yet production-wired (unlike base/wire), so this freeze is a
// drift alarm rather than a hard cross-version contract: when the OBFT refactor
// intentionally changes a 2abOBFT encoding, bump the message's version constant
// and update the matching string here in the same change.
const (
	goldenP1BHex  = "033261624f42465400000000000000000001aabbccdd0000000000000000000000000000000000000000000000000000000000000000000000070000000000003039000000020000000b68656c6c6f20776f726c640000000401020304"
	goldenVMHex   = "043261624f424654000000000000000000021122330000000000000000000000000000000000000000000000000000000000000000000000000300000000000003e7000000025630bc946d74e8aee763e228fa42d6f4bcf3f164e54c769f806c3d22e544bbd8e4240000000200000000bc946d74e8aee763e228fa42d6f4bcf3f164e54c769f806c3d22e544bbd8e424000000137369676d612d66726f6d2d6c65616465722d300000000247cfe5eb8ada0e7492d86a6b47dc93e354c32af4a3cf489e0c65067cb48277d9000000137369676d612d66726f6d2d6c65616465722d32000000106c302d7061727469616c2d627974657300000002000000010200000000000000086e722d7461672d3100000002010000000256320000000a63742d6c617965722d32"
	goldenNVMHex  = "013261624f4246540000000000000000000344550000000000000000000000000000000000000000000000000000000000000000000000000005000000000000006400000002000000010200000000000000046e722d3100000002000000000000000000"
	goldenCmtHex  = "023261624f4246540000000000000000000466770000000000000000000000000000000000000000000000000000000000000000000000000002000000000000003703000000106e722d7461672d302d7061727469616c00000001000000010100000002563100000003637431"
	goldenCertHex = "013261624f42465400000000000000000005cafebabe00000000000000000000000000000000000000000000000000000000000000000000004d00000013646563696465642076616c756520627974657300000006aabbccddeeff"
)

// goldenPhase1Bundle / goldenValueMsg / goldenNoValueMsg / goldenCommit /
// goldenCertificate are deterministic fixtures whose exact encoded bytes are
// frozen above. Between them they exercise every encoder branch that matters
// for layout: multi-entry Witnesses, all three LayerEntryKind variants
// (Empty / SigmaChained / NRPlaintext), and a Commit that carries LayerEntries
// (Side=NRDirect).

func goldenPhase1Bundle() *twoab.Phase1Bundle {
	return &twoab.Phase1Bundle{
		ClusterID:   [32]byte{0xAA, 0xBB, 0xCC, 0xDD},
		OperatorID:  7,
		Height:      12345,
		Layer:       2,
		Value:       twoab.Value("hello world"),
		LeaderSigma: twoab.Signature{0x01, 0x02, 0x03, 0x04},
	}
}

func goldenValueMsg() *twoab.ValueMsg {
	v := twoab.Value("V0")
	return &twoab.ValueMsg{
		ClusterID:  [32]byte{0x11, 0x22, 0x33},
		OperatorID: 3,
		Height:     999,
		V:          v,
		ValueRoot:  twoab.ValueRoot(v),
		Witnesses: []twoab.LayerWitness{
			{Layer: 0, ValueRoot: twoab.ValueRoot(v), Witness: twoab.Signature("sigma-from-leader-0")},
			{Layer: 2, ValueRoot: twoab.ValueRoot(twoab.Value("V2")), Witness: twoab.Signature("sigma-from-leader-2")},
		},
		L0Partial: twoab.Signature("l0-partial-bytes"),
		LayerEntries: []twoab.LayerEntry{
			{Layer: 1, Kind: twoab.LayerEntryNRPlaintext, Payload: []byte("nr-tag-1")},
			{Layer: 2, Kind: twoab.LayerEntrySigmaChained, V: twoab.Value("V2"), Payload: []byte("ct-layer-2")},
		},
	}
}

func goldenNoValueMsg() *twoab.NoValueMsg {
	return &twoab.NoValueMsg{
		ClusterID:  [32]byte{0x44, 0x55},
		OperatorID: 5,
		Height:     100,
		LayerEntries: []twoab.LayerEntry{
			{Layer: 1, Kind: twoab.LayerEntryNRPlaintext, Payload: []byte("nr-1")},
			{Layer: 2, Kind: twoab.LayerEntryEmpty},
		},
	}
}

func goldenCommit() *twoab.Commit {
	return &twoab.Commit{
		ClusterID:  [32]byte{0x66, 0x77},
		OperatorID: 2,
		Height:     55,
		Side:       twoab.CommitSideNRDirect,
		L0Partial:  twoab.Signature("nr-tag-0-partial"),
		LayerEntries: []twoab.LayerEntry{
			{Layer: 1, Kind: twoab.LayerEntrySigmaChained, V: twoab.Value("V1"), Payload: []byte("ct1")},
		},
	}
}

func goldenCertificate() *twoab.Certificate {
	return &twoab.Certificate{
		ClusterID: [32]byte{0xCA, 0xFE, 0xBA, 0xBE},
		Height:    77,
		Value:     twoab.Value("decided value bytes"),
		Signature: twoab.Signature{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	}
}

func TestGoldenEncoding(t *testing.T) {
	p1b, err := EncodePhase1Bundle(goldenPhase1Bundle())
	require.NoError(t, err)
	require.Equal(t, goldenP1BHex, hex.EncodeToString(p1b), "Phase1Bundle wire layout changed")

	vm, err := EncodeValueMsg(goldenValueMsg())
	require.NoError(t, err)
	require.Equal(t, goldenVMHex, hex.EncodeToString(vm), "ValueMsg wire layout changed")

	nvm, err := EncodeNoValueMsg(goldenNoValueMsg())
	require.NoError(t, err)
	require.Equal(t, goldenNVMHex, hex.EncodeToString(nvm), "NoValueMsg wire layout changed")

	cmt, err := EncodeCommit(goldenCommit())
	require.NoError(t, err)
	require.Equal(t, goldenCmtHex, hex.EncodeToString(cmt), "Commit wire layout changed")

	cert, err := EncodeCertificate(goldenCertificate())
	require.NoError(t, err)
	require.Equal(t, goldenCertHex, hex.EncodeToString(cert), "Certificate wire layout changed")
}
