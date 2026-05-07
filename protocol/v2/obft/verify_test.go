package obft

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// makeVerifier returns a Verifier matching the sim's stub-signer setup so
// freshly-built Phase1Bundles / Commits / Certificates verify under it.
func makeVerifier(t *testing.T, s *sim) *Verifier {
	t.Helper()
	return &Verifier{
		Signer:        NewStubSigner(s.cfg.QV(), nil), // verify-only
		TagSigner:     NewStubSigner(s.cfg.QV(), nil),
		PubKeyShares:  s.pubKeyShares,
		ClusterPubKey: nil, // stub VerifyAggregate ignores it
	}
}

func TestVerifier_Phase1Bundle_AcceptsValid(t *testing.T) {
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	b, err := s.instances[1].BuildPhase1Bundle(0, []byte("V"))
	require.NoError(t, err)
	require.NoError(t, v.VerifyPhase1Bundle(b))
}

func TestVerifier_Phase1Bundle_RejectsCorruptSigmaV(t *testing.T) {
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	b, err := s.instances[1].BuildPhase1Bundle(0, []byte("V"))
	require.NoError(t, err)
	b.SigmaV = []byte("garbage-not-a-real-partial")
	require.ErrorContains(t, v.VerifyPhase1Bundle(b), "does not verify")
}

func TestVerifier_Phase1Bundle_RejectsUnknownOperator(t *testing.T) {
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	b, err := s.instances[1].BuildPhase1Bundle(0, []byte("V"))
	require.NoError(t, err)
	// Replace OperatorID with one not in the cluster's share map.
	b.OperatorID = 99
	require.ErrorContains(t, v.VerifyPhase1Bundle(b), "no pub-key share")
}

func TestVerifier_CommitNRPartials_AcceptsValid(t *testing.T) {
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	// Build a healthy Commit from op2 that includes NR partials at every
	// non-deepest layer where op2 doesn't σ-emit. Easiest: silent-leader
	// scenario at L_0 → op2 NR's at L_0 (op1 is L_0 leader, doesn't broadcast).
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	// Don't deliver L_0 → op2 NR's at L_0.
	c, err := s.instances[2].BuildOwnCommit()
	require.NoError(t, err)
	require.NotEmpty(t, c.NRPartials, "test setup: expected NR partials")
	require.NoError(t, v.VerifyCommitNRPartials(c))
}

func TestVerifier_CommitNRPartials_RejectsCorrupt(t *testing.T) {
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	c, err := s.instances[2].BuildOwnCommit()
	require.NoError(t, err)
	require.NotEmpty(t, c.NRPartials)
	// Corrupt one NR partial.
	c.NRPartials[0].PartialSig = []byte("garbage-NR-partial")
	require.ErrorContains(t, v.VerifyCommitNRPartials(c), "does not verify")
}

func TestVerifier_Certificate_AcceptsValid(t *testing.T) {
	// Drive a healthy run so a real Output is produced, then construct a
	// Certificate from it.
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	s.runPhase2(nil)
	out, err := s.instances[1].Resolve()
	require.NoError(t, err)
	cert, err := s.instances[1].BuildCertificate(out)
	require.NoError(t, err)
	require.NoError(t, v.VerifyCertificate(cert))
}

func TestVerifier_RejectsNilSigner(t *testing.T) {
	// A misconfigured Verifier with nil Signer must error rather than panic
	// on the nil method call (defensive against caller misconfiguration).
	v := &Verifier{Signer: nil, TagSigner: NewStubSigner(3, nil)}
	require.ErrorContains(t, v.VerifyPhase1Bundle(&Phase1Bundle{}), "nil Signer")

	v = &Verifier{Signer: NewStubSigner(3, nil), TagSigner: nil}
	require.ErrorContains(t, v.VerifyCommitNRPartials(&Commit{NRPartials: []NRPartial{{Layer: 0, PartialSig: []byte("x")}}}), "nil TagSigner")
}

func TestVerifier_Certificate_RejectsCorruptSig(t *testing.T) {
	s := newSim(t, 4)
	v := makeVerifier(t, s)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	s.runPhase2(nil)
	out, err := s.instances[1].Resolve()
	require.NoError(t, err)
	cert, err := s.instances[1].BuildCertificate(out)
	require.NoError(t, err)
	cert.Signature = []byte("garbage-aggregate-sig")
	require.ErrorContains(t, v.VerifyCertificate(cert), "does not verify")
}
