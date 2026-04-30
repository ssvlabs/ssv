package tbft

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---- Config validation ---------------------------------------------------

func TestConfig_Validate_OK(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate_NoLayers(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.Layers = nil
	require.ErrorContains(t, cfg.Validate(), "no layers")
}

func TestConfig_Validate_BadF(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.F = 0
	require.ErrorContains(t, cfg.Validate(), "byzantine bound")
}

func TestConfig_Validate_ClusterTooSmall(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.Operators = cfg.Operators[:3] // n=3 with f=2 violates 3f+1
	require.ErrorContains(t, cfg.Validate(), "cluster size")
}

func TestConfig_Validate_DuplicateOperatorID(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.Operators[1] = cfg.Operators[0]
	require.ErrorContains(t, cfg.Validate(), "duplicate operator")
}

func TestConfig_Validate_DuplicateLeader(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.Layers[1].Leader = cfg.Layers[0].Leader
	require.ErrorContains(t, cfg.Validate(), "duplicate leader")
}

func TestConfig_Validate_LeaderNotMember(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.Layers[0].Leader = OperatorID(999)
	require.ErrorContains(t, cfg.Validate(), "not a cluster member")
}

func TestConfig_Validate_FetchAfterDeadline(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	cfg.Layers[0].FetchAt = cfg.Deadline + time.Second
	require.ErrorContains(t, cfg.Validate(), "FetchAt must be before deadline")
}

func TestConfig_Quorum_K(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	require.Equal(t, 5, cfg.Quorum()) // 2f+1 = 5 for f=2
	require.Equal(t, 3, cfg.K())      // K=3 for n=7
}

// ---- Tag construction ----------------------------------------------------

func TestNoQuorumTag_DeterministicAndDistinct(t *testing.T) {
	clusterA := [32]byte{0x01}
	clusterB := [32]byte{0x02}

	a1 := NoQuorumTag(clusterA, 100, 0)
	a1again := NoQuorumTag(clusterA, 100, 0)
	require.True(t, bytes.Equal(a1, a1again), "same inputs -> same tag")

	tests := []struct {
		name string
		tag  []byte
	}{
		{"different cluster", NoQuorumTag(clusterB, 100, 0)},
		{"different height", NoQuorumTag(clusterA, 101, 0)},
		{"different layer", NoQuorumTag(clusterA, 100, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.False(t, bytes.Equal(a1, tc.tag),
				"distinct context must produce distinct tag")
		})
	}
}

func TestLayerTag_Layer0IsNil(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	require.Nil(t, cfg.LayerTag(0), "layer 0 is plaintext, no tag")
}

func TestLayerTag_LayerKEqualsNoQuorumTagKMinus1(t *testing.T) {
	cfg := validProposerConfig(t, 7)
	for k := 1; k < cfg.K(); k++ {
		got := cfg.LayerTag(k)
		want := NoQuorumTag(cfg.ClusterID, cfg.Height, k-1)
		require.True(t, bytes.Equal(got, want),
			"layer %d tag should equal NoQuorumTag(layer-1)", k)
	}
}

// ---- Stub IBE + Signer round-trip ----------------------------------------

func TestStubCrypto_EndToEnd(t *testing.T) {
	// Demonstrates the Option-A composition: Signer signs/aggregates BLS
	// partial sigs on a tag; the aggregate becomes the IBE decryption key.
	signer := NewStubSigner(5)
	ibe := NewStubIBE(5)

	tag := []byte("test-tag")
	plaintext := []byte("the cluster's decided block")

	ct, err := ibe.Encrypt([]byte("clusterPubKey"), tag, plaintext)
	require.NoError(t, err)

	// 5 operators each sign the tag with their own share.
	partials := make(map[OperatorID]Signature, 5)
	for i := OperatorID(1); i <= 5; i++ {
		share := []byte{byte(i)}
		p, err := signer.SignPartial(share, tag)
		require.NoError(t, err)
		partials[i] = p
	}

	key, err := signer.AggregatePartials(partials)
	require.NoError(t, err)

	got, err := ibe.Decrypt(ct, key)
	require.NoError(t, err)
	require.True(t, bytes.Equal(plaintext, got))
}

func TestStubCrypto_BelowQuorumFails(t *testing.T) {
	signer := NewStubSigner(5)
	partials := make(map[OperatorID]Signature, 4)
	for i := OperatorID(1); i <= 4; i++ {
		p, _ := signer.SignPartial([]byte{byte(i)}, []byte("tag"))
		partials[i] = p
	}
	_, err := signer.AggregatePartials(partials)
	require.ErrorContains(t, err, "need 5 partials, got 4")
}

func TestStubCrypto_DifferentSubsetsYieldSameAggregate(t *testing.T) {
	// Critical property: any 2f+1 subset of partials must yield the SAME
	// aggregate (otherwise different operators with different received
	// subsets would compute different "decryption keys" for the same
	// ciphertext).
	signer := NewStubSigner(3)
	allPartials := make(map[OperatorID]Signature, 5)
	for i := OperatorID(1); i <= 5; i++ {
		p, _ := signer.SignPartial([]byte{byte(i)}, []byte("tag"))
		allPartials[i] = p
	}
	subset1 := map[OperatorID]Signature{1: allPartials[1], 2: allPartials[2], 3: allPartials[3]}
	subset2 := map[OperatorID]Signature{2: allPartials[2], 4: allPartials[4], 5: allPartials[5]}

	agg1, err := signer.AggregatePartials(subset1)
	require.NoError(t, err)
	agg2, err := signer.AggregatePartials(subset2)
	require.NoError(t, err)
	require.True(t, bytes.Equal(agg1, agg2),
		"distinct 2f+1 subsets of partials on the same message must aggregate to the same value")
}

func TestStubCrypto_TagMismatchAtDecrypt(t *testing.T) {
	signer := NewStubSigner(3)
	ibe := NewStubIBE(3)

	ct, err := ibe.Encrypt(nil, []byte("real-tag"), []byte("plaintext"))
	require.NoError(t, err)

	// Build a key for a different tag.
	partials := make(map[OperatorID]Signature, 3)
	for i := OperatorID(1); i <= 3; i++ {
		p, _ := signer.SignPartial([]byte{byte(i)}, []byte("wrong-tag"))
		partials[i] = p
	}
	key, err := signer.AggregatePartials(partials)
	require.NoError(t, err)

	_, err = ibe.Decrypt(ct, key)
	require.ErrorContains(t, err, "tag mismatch")
}

// ---- Test helpers --------------------------------------------------------

// validProposerConfig builds a Config that mirrors what a TBFT proposer-duty
// instance would receive at runtime, sized for cluster `n`.
func validProposerConfig(t *testing.T, n int) *Config {
	t.Helper()
	require.True(t, n == 4 || n == 7 || n == 10 || n == 13,
		"only standard SSV cluster sizes supported in tests")

	f := (n - 1) / 3
	K := 3
	if f+1 > K {
		K = f + 1
	}
	if n == 4 {
		K = 2 // TBFT2 specialization
	}

	ops := make([]OperatorID, n)
	for i := 0; i < n; i++ {
		ops[i] = OperatorID(i + 1)
	}

	layers := make([]LayerSpec, K)
	for i := 0; i < K; i++ {
		layers[i] = LayerSpec{
			Leader:  OperatorID(i + 1),
			FetchAt: 1 * time.Second, // late fetch (T_d - ~2s)
		}
	}
	if n == 4 {
		// TBFT2: layer 1 (backup) fetches early.
		layers[1].FetchAt = -4 * time.Second // T_b = T_d - ~7s before deadline
	}

	return &Config{
		Height:    1234,
		Layers:    layers,
		Deadline:  3 * time.Second,
		ClusterID: [32]byte{0xAA, 0xBB, 0xCC},
		Operators: ops,
		F:         f,
	}
}
