package wire

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	base "github.com/ssvlabs/ssv/protocol/v2/obft/base"
)

// Frozen encodings of the fixtures below. Captured before the Phase-4 wire
// primitive unification; the unification must leave these byte-for-byte
// unchanged. A diff here means the on-wire layout moved — bump the version
// constant deliberately, do not just refresh these strings.
const (
	goldenP1BHex  = "014f4246542d76310001aabbccdd0000000000000000000000000000000000000000000000000000000000000000000000070000000000003039000000020000000b68656c6c6f20776f726c640000000401020304"
	goldenCmtHex  = "014f4246542d763100021122330000000000000000000000000000000000000000000000000000000000000000000000000300000000000003e7000400000002563000000003637430000000000000000000000002563200000016636970686572746578742d666f722d6c617965722d320000000a56332d646565706573740000000243540001000000010000000e6e722d7369672d6c617965722d310002000000000000000000000001bc946d74e8aee763e228fa42d6f4bcf3f164e54c769f806c3d22e544bbd8e424000000137369676d612d66726f6d2d6c65616465722d3100000002000000000000000347cfe5eb8ada0e7492d86a6b47dc93e354c32af4a3cf489e0c65067cb48277d9000000137369676d612d66726f6d2d6c65616465722d33"
	goldenCertHex = "014f4246542d76310003cafebabe00000000000000000000000000000000000000000000000000000000000000000000004d00000013646563696465642076616c756520627974657300000006aabbccddeeff"
)

// goldenPhase1Bundle / goldenCommit / goldenCertificate are deterministic
// fixtures whose exact encoded bytes are frozen below. base is production-
// wired: any change to the on-wire layout is a cross-version compatibility
// break that MUST be a deliberate version bump. These golden tests make such a
// change loud (the Phase-4 primitive unification preserved byte-for-byte
// output — that is exactly what these guard).

func goldenPhase1Bundle() *base.Phase1Bundle {
	return &base.Phase1Bundle{
		ClusterID:   [32]byte{0xAA, 0xBB, 0xCC, 0xDD},
		OperatorID:  7,
		Height:      12345,
		Layer:       2,
		Value:       []byte("hello world"),
		LeaderSigma: []byte{0x01, 0x02, 0x03, 0x04},
	}
}

func goldenCommit() *base.Commit {
	return &base.Commit{
		ClusterID:  [32]byte{0x11, 0x22, 0x33},
		OperatorID: 3,
		Height:     999,
		Layers: []base.EncryptedLayer{
			{Value: []byte("V0"), Ciphertext: []byte("ct0")},
			{},
			{Value: []byte("V2"), Ciphertext: []byte("ciphertext-for-layer-2")},
			{Value: []byte("V3-deepest"), Ciphertext: []byte("CT")},
		},
		NRPartials: []base.NRPartial{
			{Layer: 1, PartialSig: []byte("nr-sig-layer-1")},
		},
		Witnesses: []base.LeaderSigmaWitness{
			{Layer: 0, Leader: 1, ValueRoot: base.ValueRoot([]byte("V0")), Sigma: []byte("sigma-from-leader-1")},
			{Layer: 2, Leader: 3, ValueRoot: base.ValueRoot([]byte("V2")), Sigma: []byte("sigma-from-leader-3")},
		},
	}
}

func goldenCertificate() *base.Certificate {
	return &base.Certificate{
		ClusterID: [32]byte{0xCA, 0xFE, 0xBA, 0xBE},
		Height:    77,
		Value:     []byte("decided value bytes"),
		Signature: []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
	}
}

func TestGoldenEncoding(t *testing.T) {
	p1b, err := EncodePhase1Bundle(goldenPhase1Bundle())
	require.NoError(t, err)
	require.Equal(t, goldenP1BHex, hex.EncodeToString(p1b), "Phase1Bundle wire layout changed")

	cmt, err := EncodeCommit(goldenCommit())
	require.NoError(t, err)
	require.Equal(t, goldenCmtHex, hex.EncodeToString(cmt), "Commit wire layout changed")

	cert, err := EncodeCertificate(goldenCertificate())
	require.NoError(t, err)
	require.Equal(t, goldenCertHex, hex.EncodeToString(cert), "Certificate wire layout changed")
}
