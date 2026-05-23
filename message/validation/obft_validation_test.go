package validation

import (
	"context"
	"fmt"
	"testing"
	"time"

	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
)

// G1 regression tests for validateOBFTMessage: confirm that BLS / IBE-tag
// verification at the validation boundary rejects messages with garbage
// cryptographic material before they reach the consensus path. Shared
// helpers (obftTestSetup, signOBFTEnvelope, etc.) live in
// obft_test_helpers_test.go.

// ---- Phase1Bundle ----

func TestValidateOBFT_Phase1Bundle_AcceptsValidSigma(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	// Sign over the proposer-domain signing root via NewProposerSigner —
	// matches the production V-side signer wiring.
	innerSigner := blsbackend.New(ks.Shares[signer].Serialize())
	vSigner, err := obftadapter.NewProposerSigner(innerSigner, mv.netCfg.Beacon)
	require.NoError(t, err)
	v := proposerCandidateV()
	sigV, err := vSigner.SignPartial(v)
	require.NoError(t, err)
	bundle := &obftcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  obftcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       v,
		LeaderSigma: sigV,
	}
	body, err := wire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	env, err := mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)
	require.NotNil(t, env)
	require.Equal(t, wire.KindPhase1Bundle, env.Kind)
}

func TestValidateOBFT_Phase1Bundle_RejectsCorruptSigmaV(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	// V is a well-formed block (so signingRootFor succeeds); LeaderSigma is
	// garbage — exercises the actual partial-sig verification failure.
	bundle := &obftcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  obftcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       proposerCandidateV(),
		LeaderSigma: []byte("garbage-not-a-valid-bls-partial--padded-to-some-length-like-real-sigs"),
	}
	body, err := wire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "phase-1 bundle verification")
}

func TestValidateOBFT_Phase1Bundle_RejectsInnerOuterSignerMismatch(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	innerSigner := share.Committee[0].Signer
	outerSigner := share.Committee[1].Signer
	require.NotEqual(t, innerSigner, outerSigner)

	// Properly sign with INNER's share, but ship under OUTER's identity —
	// the validator should reject before any BLS verification (inner-signer
	// mismatch is the first guard).
	blsSigner := blsbackend.New(ks.Shares[innerSigner].Serialize())
	v := []byte("V")
	sigV, err := blsSigner.SignPartial(v)
	require.NoError(t, err)
	bundle := &obftcore.Phase1Bundle{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(innerSigner),
		Height:     obftTestHeight(mv), Layer: 0,
		Value: v, LeaderSigma: sigV,
	}
	body, err := wire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, outerSigner)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "outer signer")
}

// ---- Commit (NR partials) ----

func TestValidateOBFT_Commit_AcceptsValidNRPartials(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	// Forge a Commit with valid NR partials. NR uses tagSigner (Kyber-DST).
	tagSigner := blsbackend.NewKyberSigner(ks.Shares[signer].Serialize())
	slot := obftTestHeight(mv)
	tag := obftcore.NoQuorumTag(clusterID, slot, 0)
	nrSig, err := tagSigner.SignPartial(tag)
	require.NoError(t, err)

	commit := &obftcore.Commit{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(signer),
		Height:     slot,
		Layers:     make([]obftcore.EncryptedLayer, 4),
		NRPartials: []obftcore.NRPartial{{Layer: 0, PartialSig: nrSig}},
	}
	body, err := wire.WrapCommit(commit)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	env, err := mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)
	require.Equal(t, wire.KindCommit, env.Kind)
}

func TestValidateOBFT_Commit_RejectsCorruptNRPartial(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	commit := &obftcore.Commit{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		Layers:     make([]obftcore.EncryptedLayer, 4),
		NRPartials: []obftcore.NRPartial{{Layer: 0, PartialSig: []byte("garbage-NR-padded-to-something-resembling-bls-length-bytes-bytes")}},
	}
	body, err := wire.WrapCommit(commit)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "NR-partial verification")
}

// ---- Certificate ----

// The "happy path" certificate test would require a fully-aggregated BLS
// signature that verifies against the cluster pubkey — out of scope for a
// validator-layer unit test. Cover the rejection path instead, which is
// the security-relevant one.
//
// Per spec §Phase 2 wire format, witnesses ship value_root + σ_V (no full
// V); standalone Verifier is structural-only (it can't reproduce σ_V
// verification without V). So garbage σ_V is NOT rejected here — it's
// dropped at the protocol layer when value_root doesn't match any retained
// V. This test now covers the structural rejection: an unknown leader.
func TestValidateOBFT_Commit_RejectsStructurallyInvalidWitness(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	commit := &obftcore.Commit{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		Layers:     make([]obftcore.EncryptedLayer, 4),
		Witnesses: []obftcore.LeaderSigmaWitness{
			// Leader 999 is not in the cluster — structural rejection.
			{Layer: 0, Leader: 999, ValueRoot: obftcore.ValueRoot(proposerCandidateV()), Sigma: []byte("witness-sig-padded-to-resemble-bls-length-12345")},
		},
	}
	body, err := wire.WrapCommit(commit)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.Error(t, err, "Commit with structurally invalid witness should be rejected")
}

func TestValidateOBFT_Certificate_RejectsCorruptAggregate(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	cert := &obftcore.Certificate{
		ClusterID: clusterID,
		Height:    obftTestHeight(mv),
		Value:     []byte("decided-V"),
		Signature: []byte("garbage-aggregate-not-a-valid-bls-sig"),
	}
	body, err := wire.WrapCertificate(cert)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "certificate verification")
}

// Integration: identical bytes redelivered through validateOBFTMessage are
// rejected by the admission tracker before BLS verification fires. Saves
// the BLS cost on gossipsub re-delivery floods.
func TestValidateOBFT_Admissions_RejectsIdenticalRedelivery(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	// Build a valid Phase-1 bundle so BLS would otherwise pass — proving
	// admission rejects PRE-BLS even for cryptographically-valid bytes.
	innerSigner := blsbackend.New(ks.Shares[signer].Serialize())
	vSigner, err := obftadapter.NewProposerSigner(innerSigner, mv.netCfg.Beacon)
	require.NoError(t, err)
	v := proposerCandidateV()
	sigV, err := vSigner.SignPartial(v)
	require.NoError(t, err)
	bundle := &obftcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  obftcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       v,
		LeaderSigma: sigV,
	}
	body, err := wire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)
	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()

	// First delivery admitted.
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)

	// Identical re-broadcast rejected at admission, before BLS.
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "identical content")
}

// Integration: distinct bytes from the same operator within the bucket cap
// are admitted (so the protocol's Rule 2/3 equivocation paths can fire);
// past the cap, further distinct bytes are rejected pre-BLS.
func TestValidateOBFT_Admissions_BucketCapEnforced(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer
	peerID, _ := libp2ptest.RandPeerID()

	// Use Commit envelopes since they're cheap to vary by changing
	// NRPartials bytes — no need for valid BLS sigs (validation will
	// fail at NR-verify, but admission runs first and counts).
	emit := func(varyByte byte) error {
		commit := &obftcore.Commit{
			ClusterID:  clusterID,
			OperatorID: obftcore.OperatorID(signer),
			Height:     obftTestHeight(mv),
			Layers:     make([]obftcore.EncryptedLayer, 4),
			NRPartials: []obftcore.NRPartial{{
				Layer:      0,
				PartialSig: []byte{varyByte, 0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01, 0x02, 0x03, 0x04},
			}},
		}
		body, err := wire.WrapCommit(commit)
		require.NoError(t, err)
		msg := signOBFTEnvelope(t, msgID, body, signer)
		_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
		return err
	}

	// Up to MaxDistinctPerOpSlot distinct emissions are admitted (each
	// fails BLS-verify downstream — that's a different error than
	// "too many distinct messages").
	for k := 0; k < 8; k++ { // matches consensusValidationMaxDistinctPerOpSlot
		err := emit(byte(k))
		require.NotContains(t, fmt.Sprint(err), "too many distinct messages",
			"emission %d should NOT hit the bucket cap", k)
	}

	// 9th distinct emission rejected at admission, pre-BLS.
	err := emit(0xFF)
	require.ErrorContains(t, err, "too many distinct messages")
}
