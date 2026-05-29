package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the Config.SkipNRPartialReverify gate (audit finding F5). Mirrors
// obft/base/phase2_skipverify_test.go, which documents the safety contract.
//
// twoab's F5 gate lives inside Instance.verifyNRTagPartial (instance.go), the
// single chokepoint for all 5 NR-tag verify call sites (phase2a / phase2b).
// When the flag is true, verifyNRTagPartial returns true after the pub-share
// lookup but skips the BLS verify itself. The flag defaults false; only the
// production runner sets it true (the validation-layer Verifier at
// message/validation/twoab_validation.go already ran the verify upstream).

// TestTwoab_SkipNRPartialReverify_DefaultStillVerifies — with the default
// (zero) Config.SkipNRPartialReverify, a Commit carrying a malformed
// nr_tag_0 L0Partial MUST be rejected by the in-Instance verify:
// addToNrTagPool is only called when verifyNRTagPartial returns true. We
// assert the malformed op never appears in nrTagPool[0].
func TestTwoab_SkipNRPartialReverify_DefaultStillVerifies(t *testing.T) {
	s := newSim(t, 4)
	require.False(t, s.cfg.SkipNRPartialReverify,
		"newSim must leave the flag at its safe default")
	receiver := s.instances[3]

	// Build op2's nr_tag_0 partial then tamper with it.
	signer := NewStubSigner(s.cfg.QV(), s.pubShares[2])
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := signer.SignPartial(tag)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, nrSig...)

	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  malformed,
	}
	require.NoError(t, receiver.ObserveCommit(c))

	// Default flag → verify fails → op2 is NOT pooled.
	pool := receiver.nrTagPool[0]
	_, pooled := pool[2]
	require.False(t, pooled,
		"default-false flag must reject malformed L0Partial — op MUST NOT enter nrTagPool")
}

// TestTwoab_SkipNRPartialReverify_TrueBypassesVerify — with the flag set
// true, verifyNRTagPartial short-circuits and op IS pooled even on
// malformed bytes. In production this is safe because the validation layer's
// Verifier already ran the verify and would have rejected the envelope
// before it reached Instance. The test just confirms the flag actually
// skips the call — it does NOT model the upstream validation; this is the
// safety hole that the "MUST keep the flag false in any path that doesn't
// run the upstream Verifier" contract closes.
func TestTwoab_SkipNRPartialReverify_TrueBypassesVerify(t *testing.T) {
	s := newSim(t, 4)
	skipCfg := *s.cfg
	skipCfg.SkipNRPartialReverify = true

	receiver, err := NewInstance(
		&skipCfg, 4,
		NewStubSigner(s.cfg.QV(), s.pubShares[4]),
		NewStubSigner(s.cfg.QV(), s.pubShares[4]),
		NewStubIBE(s.cfg.QEnc()),
		[]byte{0xCC, 0xDD},
		s.pubShares,
		nil, nil,
	)
	require.NoError(t, err)

	sender := NewStubSigner(s.cfg.QV(), s.pubShares[2])
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := sender.SignPartial(tag)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, nrSig...)

	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  malformed,
	}
	require.NoError(t, receiver.ObserveCommit(c))

	pool := receiver.nrTagPool[0]
	_, pooled := pool[2]
	require.True(t, pooled,
		"flag true must skip verify → malformed partial still pooled (production has upstream Verifier as backstop)")
}
