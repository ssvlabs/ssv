package validation

import (
	"context"
	"testing"
	"time"

	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	twoabwire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/twoab"
)

// Validation-boundary tests for validateTwoabMessage: confirm the 2abOBFT
// inner-crypto verification (via the L5 twoab.Verifier) accepts well-formed
// material and rejects garbage before it reaches the consensus path. Reuses
// the variant-neutral OBFT helpers (obftTestSetup / obftCommitteeInfo /
// obftTestHeight / proposerCandidateV) from obft_test_helpers_test.go — only
// the wire codec, message shapes, and the SSV2abOBFTMsgType envelope differ.

// signTwoabEnvelope wraps a wire-encoded 2abOBFT envelope body in a
// SignedSSVMessage under the given outer signer (SSV2abOBFTMsgType). The outer
// RSA signature is a fixed placeholder — validateTwoabMessage verifies only the
// inner BLS / IBE-tag material.
func signTwoabEnvelope(t testing.TB, msgID spectypes.MessageID, body []byte, signer spectypes.OperatorID) *spectypes.SignedSSVMessage {
	t.Helper()
	return &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{signer},
		Signatures:  [][]byte{make([]byte, 256)},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: ssvmessage.SSV2abOBFTMsgType,
			MsgID:   msgID,
			Data:    body,
		},
	}
}

// ---- Phase1Bundle (shared obft type) ----

func TestValidateTwoab_Phase1Bundle_AcceptsValidSigma(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	vSigner, err := twoabadapter.NewProposerSigner(blsbackend.New(ks.Shares[signer].Serialize()), mv.netCfg.Beacon)
	require.NoError(t, err)
	v := proposerCandidateV()
	sigV, err := vSigner.SignPartial(v)
	require.NoError(t, err)
	bundle := &twoabcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  twoabcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       v,
		LeaderSigma: sigV,
	}
	body, err := twoabwire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	env, err := mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)
	require.NotNil(t, env)
	require.Equal(t, twoabwire.KindPhase1Bundle, env.Kind)
}

func TestValidateTwoab_Phase1Bundle_RejectsCorruptSigma(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	bundle := &twoabcore.Phase1Bundle{
		ClusterID:   clusterID,
		OperatorID:  twoabcore.OperatorID(signer),
		Height:      obftTestHeight(mv),
		Layer:       0,
		Value:       proposerCandidateV(),
		LeaderSigma: []byte("garbage-not-a-valid-bls-partial--padded-to-some-length-like-real-sigs"),
	}
	body, err := twoabwire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "phase-1 bundle verification")
}

func TestValidateTwoab_Phase1Bundle_RejectsInnerOuterSignerMismatch(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	innerSigner := share.Committee[0].Signer
	outerSigner := share.Committee[1].Signer
	require.NotEqual(t, innerSigner, outerSigner)

	vSigner, err := twoabadapter.NewProposerSigner(blsbackend.New(ks.Shares[innerSigner].Serialize()), mv.netCfg.Beacon)
	require.NoError(t, err)
	v := proposerCandidateV()
	sigV, err := vSigner.SignPartial(v)
	require.NoError(t, err)
	bundle := &twoabcore.Phase1Bundle{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(innerSigner),
		Height:     obftTestHeight(mv), Layer: 0,
		Value: v, LeaderSigma: sigV,
	}
	body, err := twoabwire.WrapPhase1Bundle(bundle)
	require.NoError(t, err)

	// Properly signed by inner, shipped under outer's identity → rejected at
	// the inner-signer guard before any BLS verify.
	msg := signTwoabEnvelope(t, msgID, body, outerSigner)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "outer signer")
}

// ---- ValueMsg (2ab-specific: σ on L0Partial) ----

func TestValidateTwoab_ValueMsg_AcceptsValidL0Partial(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	vSigner, err := twoabadapter.NewProposerSigner(blsbackend.New(ks.Shares[signer].Serialize()), mv.netCfg.Beacon)
	require.NoError(t, err)
	v := proposerCandidateV()
	l0, err := vSigner.SignPartial(v)
	require.NoError(t, err)
	vm := &twoabcore.ValueMsg{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		V:          v,
		ValueRoot:  twoabcore.ValueRoot(v),
		L0Partial:  l0,
	}
	body, err := twoabwire.WrapValueMsg(vm)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	env, err := mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)
	require.Equal(t, twoabwire.KindValue, env.Kind)
}

func TestValidateTwoab_ValueMsg_RejectsCorruptL0Partial(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	vm := &twoabcore.ValueMsg{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		V:          proposerCandidateV(),
		ValueRoot:  twoabcore.ValueRoot(proposerCandidateV()),
		L0Partial:  []byte("garbage-l0-partial-padded-to-resemble-a-real-bls-signature-1234567"),
	}
	body, err := twoabwire.WrapValueMsg(vm)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "value-msg verification")
}

// ---- NoValueMsg (2ab-specific: NRPlaintext layer entries) ----

func TestValidateTwoab_NoValueMsg_AcceptsValidNRPlaintext(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	tagSigner := blsbackend.NewKyberSigner(ks.Shares[signer].Serialize())
	height := obftTestHeight(mv)
	nrSig, err := tagSigner.SignPartial(twoabcore.NoQuorumTag(clusterID, height, 1))
	require.NoError(t, err)
	nv := &twoabcore.NoValueMsg{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(signer),
		Height:     height,
		LayerEntries: []twoabcore.LayerEntry{
			{Layer: 1, Kind: twoabcore.LayerEntryNRPlaintext, Payload: nrSig},
		},
	}
	body, err := twoabwire.WrapNoValueMsg(nv)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	env, err := mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)
	require.Equal(t, twoabwire.KindNoValue, env.Kind)
}

// ---- Commit (nr_tag_0 on L0Partial) ----

func TestValidateTwoab_Commit_AcceptsValidNRPartial(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	tagSigner := blsbackend.NewKyberSigner(ks.Shares[signer].Serialize())
	height := obftTestHeight(mv)
	nrSig, err := tagSigner.SignPartial(twoabcore.NoQuorumTag(clusterID, height, 0))
	require.NoError(t, err)
	commit := &twoabcore.Commit{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(signer),
		Height:     height,
		Side:       twoabcore.CommitSideNR,
		L0Partial:  nrSig,
	}
	body, err := twoabwire.WrapCommit(commit)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	env, err := mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)
	require.Equal(t, twoabwire.KindCommit, env.Kind)
}

func TestValidateTwoab_Commit_RejectsCorruptNRPartial(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	commit := &twoabcore.Commit{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		Side:       twoabcore.CommitSideNR,
		L0Partial:  []byte("garbage-NR-padded-to-something-resembling-bls-length-bytes-bytes"),
	}
	body, err := twoabwire.WrapCommit(commit)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "commit verification")
}

// ---- Certificate (rejection path; happy path needs a full aggregate) ----

func TestValidateTwoab_Certificate_RejectsCorruptAggregate(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	cert := &twoabcore.Certificate{
		ClusterID: clusterID,
		Height:    obftTestHeight(mv),
		Value:     []byte("decided-V"),
		Signature: []byte("garbage-aggregate-not-a-valid-bls-sig"),
	}
	body, err := twoabwire.WrapCertificate(cert)
	require.NoError(t, err)

	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "certificate verification")
}

// ---- admission tracker (reused, kind-generic) ----

// Identical bytes redelivered through validateTwoabMessage are rejected by the
// shared admission tracker before BLS verification fires.
func TestValidateTwoab_Admissions_RejectsIdenticalRedelivery(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	vSigner, err := twoabadapter.NewProposerSigner(blsbackend.New(ks.Shares[signer].Serialize()), mv.netCfg.Beacon)
	require.NoError(t, err)
	v := proposerCandidateV()
	l0, err := vSigner.SignPartial(v)
	require.NoError(t, err)
	vm := &twoabcore.ValueMsg{
		ClusterID:  clusterID,
		OperatorID: twoabcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		V:          v,
		ValueRoot:  twoabcore.ValueRoot(v),
		L0Partial:  l0,
	}
	body, err := twoabwire.WrapValueMsg(vm)
	require.NoError(t, err)
	msg := signTwoabEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()

	// First delivery admitted.
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.NoError(t, err)

	// Identical re-broadcast rejected at admission, before BLS.
	_, err = mv.validateTwoabMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "identical content")
}
