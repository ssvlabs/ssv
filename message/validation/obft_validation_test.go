package validation

import (
	"context"
	"testing"
	"time"

	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/networkconfig"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
)

// G1 regression tests for validateOBFTMessage: confirm that BLS / IBE-tag
// verification at the validation boundary rejects messages with garbage
// cryptographic material before they reach the consensus path.

// obftTestHeight returns a Height inside the validation layer's accepted
// slot window. Use this for any envelope.Height value in tests; raw constants
// like 200 fail the slot-window check.
func obftTestHeight(mv *messageValidator) obftcore.Height {
	return obftcore.Height(mv.netCfg.EstimatedCurrentSlot())
}

func obftTestSetup(t *testing.T) (
	*messageValidator,
	*spectestingutils.TestKeySet,
	*ssvtypes.SSVShare,
	spectypes.MessageID,
	[32]byte,
) {
	t.Helper()
	ks := spectestingutils.Testing4SharesSet()
	share := &ssvtypes.SSVShare{
		Share: *spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex),
	}

	ctrl := gomock.NewController(t)
	validatorStore := mocks.NewMockValidatorStore(ctrl)
	validatorStore.EXPECT().Validator(gomock.Any()).
		DoAndReturn(func(pubKey []byte) (*ssvtypes.SSVShare, bool) {
			return share, true
		}).AnyTimes()
	validatorStore.EXPECT().Committee(gomock.Any()).Return(nil, false).AnyTimes()

	mv := &messageValidator{
		netCfg:         networkconfig.TestNetwork,
		validatorStore: validatorStore,
	}
	msgID := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, share.ValidatorPubKey[:], spectypes.RoleProposer)
	clusterID := [32]byte{0xAA, 0xBB}
	return mv, ks, share, msgID, clusterID
}

func obftCommitteeInfo(share *ssvtypes.SSVShare) CommitteeInfo {
	ops := make([]spectypes.OperatorID, 0, len(share.Committee))
	for _, m := range share.Committee {
		ops = append(ops, m.Signer)
	}
	return newCommitteeInfo(spectypes.CommitteeID{}, ops, nil)
}

// signOBFTEnvelope wraps an envelope in a SignedSSVMessage with an outer
// signer. We bypass real RSA signing — validateOBFTMessage doesn't verify
// the outer signature itself (that's done by the upper layer); it only
// checks structural and BLS-inner correctness.
func signOBFTEnvelope(t *testing.T, msgID spectypes.MessageID, body []byte, signer spectypes.OperatorID) *spectypes.SignedSSVMessage {
	t.Helper()
	return &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{signer},
		Signatures:  [][]byte{make([]byte, 256)},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: 4, // SSVOBFTMsgType (avoid import cycle)
			MsgID:   msgID,
			Data:    body,
		},
	}
}

// ---- Phase1Bundle ----

func TestValidateOBFT_Phase1Bundle_AcceptsValidSigma(t *testing.T) {
	mv, ks, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	// Build a Phase1Bundle signed correctly by the operator's BLS share.
	blsSigner := blsbackend.New(ks.Shares[signer].Serialize())
	v := []byte("hello-V")
	sigV, err := blsSigner.SignPartial(v)
	require.NoError(t, err)
	bundle := &obftcore.Phase1Bundle{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		Layer:      0,
		Value:      v,
		SigmaV:     sigV,
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

	bundle := &obftcore.Phase1Bundle{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		Layer:      0,
		Value:      []byte("V"),
		SigmaV:     []byte("garbage-not-a-valid-bls-partial--padded-to-some-length-like-real-sigs"),
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
		Value: v, SigmaV: sigV,
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
func TestValidateOBFT_Commit_RejectsCorruptWitness(t *testing.T) {
	mv, _, share, msgID, clusterID := obftTestSetup(t)
	signer := share.Committee[0].Signer

	// A Commit whose Witnesses include a garbage σ_V — must be rejected
	// at validation, before reaching the protocol layer's expensive
	// rehydration path.
	commit := &obftcore.Commit{
		ClusterID:  clusterID,
		OperatorID: obftcore.OperatorID(signer),
		Height:     obftTestHeight(mv),
		Layers:     make([]obftcore.EncryptedLayer, 4),
		Witnesses: []obftcore.LeaderSigmaWitness{
			{Layer: 0, Leader: obftcore.OperatorID(signer), Value: []byte("V"), SigmaV: []byte("garbage-witness-sig-padded-to-resemble-bls-length")},
		},
	}
	body, err := wire.WrapCommit(commit)
	require.NoError(t, err)

	msg := signOBFTEnvelope(t, msgID, body, signer)
	peerID, _ := libp2ptest.RandPeerID()
	_, err = mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
	require.ErrorContains(t, err, "witness verification")
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
