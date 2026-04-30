package tbft

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- ValidateOnion -------------------------------------------------------

func TestValidateOnion_OK(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	require.NoError(t, ValidateOnion(o, cfg))
}

func TestValidateOnion_NilOnion(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	require.ErrorContains(t, ValidateOnion(nil, cfg), "nil onion")
}

func TestValidateOnion_NilConfig(t *testing.T) {
	require.ErrorContains(t, ValidateOnion(&Onion{}, nil), "nil config")
}

func TestValidateOnion_HeightMismatch(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	o.Height = cfg.Height + 1
	require.ErrorContains(t, ValidateOnion(o, cfg), "height")
}

func TestValidateOnion_WrongLayerCount(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	o.Layers = o.Layers[:cfg.K()-1]
	require.ErrorContains(t, ValidateOnion(o, cfg), "layers, expected K=")
}

func TestValidateOnion_SenderNotInCluster(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	o.OperatorID = OperatorID(999)
	require.ErrorContains(t, ValidateOnion(o, cfg), "not in cluster")
}

func TestValidateOnion_HalfPopulatedLayer(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	// Make layer 1 half-populated: keep Value, clear Ciphertext.
	o.Layers[1].Ciphertext = nil
	require.ErrorContains(t, ValidateOnion(o, cfg), "half-populated")
}

func TestValidateOnion_TagMismatch(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	// Layer 1's tag should be NoQuorumTag(0). Replace with garbage.
	o.Layers[1].Tag = []byte("not-the-right-tag")
	require.ErrorContains(t, ValidateOnion(o, cfg), "tag mismatch")
}

func TestValidateOnion_EmptyLayerOK(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	o := buildHealthyOnion(t, cfg, OperatorID(1))
	// Clear layer 1 entirely (operator did not contribute at layer 1).
	o.Layers[1] = EncryptedLayer{}
	require.NoError(t, ValidateOnion(o, cfg))
}

// ---- ValidateNonReceipt --------------------------------------------------

func TestValidateNonReceipt_OK(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	signer := NewStubSigner(cfg.Quorum())
	nr, err := BuildNonReceipt(cfg, OperatorID(2), []byte{2}, 0, signer)
	require.NoError(t, err)
	require.NoError(t, ValidateNonReceipt(nr, cfg))
}

func TestValidateNonReceipt_HeightMismatch(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	signer := NewStubSigner(cfg.Quorum())
	nr, _ := BuildNonReceipt(cfg, OperatorID(2), []byte{2}, 0, signer)
	nr.Height = cfg.Height + 1
	require.ErrorContains(t, ValidateNonReceipt(nr, cfg), "height")
}

func TestValidateNonReceipt_LayerOutOfRange(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	signer := NewStubSigner(cfg.Quorum())
	nr, _ := BuildNonReceipt(cfg, OperatorID(2), []byte{2}, 0, signer)
	// Last layer (K-1) is invalid for non-receipt.
	nr.Layer = cfg.K() - 1
	require.ErrorContains(t, ValidateNonReceipt(nr, cfg), "out of valid range")
}

func TestValidateNonReceipt_SenderNotInCluster(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	signer := NewStubSigner(cfg.Quorum())
	nr, _ := BuildNonReceipt(cfg, OperatorID(2), []byte{2}, 0, signer)
	nr.OperatorID = OperatorID(999)
	require.ErrorContains(t, ValidateNonReceipt(nr, cfg), "not in cluster")
}

func TestValidateNonReceipt_EmptyPartialSig(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	signer := NewStubSigner(cfg.Quorum())
	nr, _ := BuildNonReceipt(cfg, OperatorID(2), []byte{2}, 0, signer)
	nr.PartialSig = nil
	require.ErrorContains(t, ValidateNonReceipt(nr, cfg), "empty PartialSig")
}

// ---- helper to build a "healthy" onion for validation tests --------------

func buildHealthyOnion(t *testing.T, cfg *Config, opID OperatorID) *Onion {
	t.Helper()
	signer := NewStubSigner(cfg.Quorum())
	ibe := NewStubIBE(cfg.Quorum())

	// Provide a candidate value at every layer.
	candidates := make(map[int]Value, cfg.K())
	for k := 0; k < cfg.K(); k++ {
		candidates[k] = Value("layer-value-")
	}

	o, err := BuildOnion(cfg, opID, []byte{byte(opID)}, []byte("clusterPK"), candidates, signer, ibe)
	require.NoError(t, err)
	return o
}
