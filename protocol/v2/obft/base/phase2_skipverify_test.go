package base

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
)

// Tests for the Config.SkipNRPartialReverify gate (audit finding F5). The
// safety contract is documented on the SkipNRPartialReverify field.
//
// The flag exists because the validation layer
// (message/validation/obft_validation.go) runs the runner's
// Verifier.VerifyCommitNRPartials on every inbound KindCommit envelope
// before dispatch hands the Commit to Instance — the in-Instance repeat
// is pure defense-in-depth for paths that bypass that validation
// (consensustest, ad-hoc test harnesses). The flag defaults false so any
// unknown / unaudited code path keeps the in-Instance verify active; only
// the production runner sets it true.

// TestObft_SkipNRPartialReverify_DefaultStillVerifies — with the default
// (zero) Config.SkipNRPartialReverify, ObserveCommit MUST run the in-Instance
// NR-partial BLS verify and reject a Commit carrying a malformed NR partial.
// Guards against the F5 production-only optimization accidentally being
// applied to test paths that don't have the upstream Verifier as backstop.
func TestObft_SkipNRPartialReverify_DefaultStillVerifies(t *testing.T) {
	s := newSim(t, 4) // newSim leaves SkipNRPartialReverify at default false
	require.False(t, s.cfg.SkipNRPartialReverify,
		"newSim must leave the flag at its safe default")
	receiver := s.instances[3]

	// Build a Commit from op2 with a malformed NR partial — prepend a byte
	// to corrupt the BLS sig. The in-Instance verifyCommitNRPartials must
	// reject it.
	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	tag := obft.NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := signer.SignPartial(tag)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, nrSig...)
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{{Layer: 0, PartialSig: malformed}},
	}
	err = receiver.ObserveCommit(c)
	require.ErrorContains(t, err, "failed verification",
		"default-false flag must reject malformed NR partial")
}

// TestObft_SkipNRPartialReverify_TrueBypassesVerify — with the flag set
// true, ObserveCommit MUST NOT run the in-Instance NR-partial BLS verify, so
// a Commit that would otherwise be rejected for a malformed NR partial is
// accepted. In production this is safe because the validation layer's
// Verifier.VerifyCommitNRPartials already ran the verify and would have
// rejected the envelope before it reached Instance. The test just confirms
// the flag actually skips the call — it does NOT model the upstream
// validation; this is the safety hole that the "MUST keep the flag false in
// any path that doesn't run the upstream Verifier" contract closes.
func TestObft_SkipNRPartialReverify_TrueBypassesVerify(t *testing.T) {
	s := newSim(t, 4)

	// Construct a sibling Instance for op4 with the skip flag set. Use a
	// copy of s.cfg so the other sim instances aren't affected.
	skipCfg := *s.cfg
	skipCfg.SkipNRPartialReverify = true
	signer := NewStubSigner(s.cfg.QV(), []byte{4})
	ibe := NewStubIBE(s.cfg.QV())
	receiver, err := NewInstance(
		&skipCfg, 4,
		signer, signer, ibe,
		[]byte{0xCC, 0xDD}, s.pubKeyShares, nil, nil,
	)
	require.NoError(t, err)

	// Same malformed Commit as the default test. With the flag set, the
	// in-Instance verifyCommitNRPartials is skipped and ObserveCommit returns
	// nil error (it still processes the σ-side of the Commit normally — there
	// just is no σ entry here, so the call ends up as a near-noop).
	sender := NewStubSigner(s.cfg.QV(), []byte{2})
	tag := obft.NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := sender.SignPartial(tag)
	require.NoError(t, err)
	malformed := append([]byte{0xFF}, nrSig...)
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{{Layer: 0, PartialSig: malformed}},
	}
	require.NoError(t, receiver.ObserveCommit(c),
		"flag true must skip the verify and accept the Commit")
}
