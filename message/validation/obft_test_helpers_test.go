package validation

// Shared test helpers for OBFT validation tests (unit tests in
// obft_validation_test.go and obft_admissions_test.go, plus fuzz tests in
// obft_fuzz_test.go). Lives in a *_test.go file so it's not compiled into
// production builds.

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
)

// obftTestClusterID is the ClusterID stamped into every OBFT test message.
// The verifier doesn't enforce a specific cluster ID at the validation
// boundary, so the value is arbitrary — picked once here for consistency
// across unit and fuzz tests.
var obftTestClusterID = [32]byte{0xAA, 0xBB}

// obftTestHeight returns a Height inside the validation layer's accepted
// slot window. Use this for any envelope.Height value in tests; raw
// constants like 200 fail the slot-window check.
func obftTestHeight(mv *messageValidator) obftcore.Height {
	return obftcore.Height(mv.netCfg.EstimatedCurrentSlot())
}

// obftTestSetup builds a messageValidator wired to a 4-share testing
// keyset. The returned values are:
//
//   - mv: the validator (fresh admission tracker; no state from prior calls).
//   - ks: the keyset (use for signing partials in tests).
//   - share: the validator's SSVShare (committee, pubkey, etc.).
//   - msgID: the proposer-role MessageID for this validator.
//   - clusterID: obftTestClusterID, returned for caller convenience.
//
// Callable from any *testing.T / *testing.F via the testing.TB interface.
func obftTestSetup(t testing.TB) (
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
		netCfg:              networkconfig.TestNetwork,
		validatorStore:      validatorStore,
		consensusAdmissions: newConsensusAdmissionTracker(),
	}
	msgID := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, share.ValidatorPubKey[:], spectypes.RoleProposer)
	return mv, ks, share, msgID, obftTestClusterID
}

// obftCommitteeInfo builds a CommitteeInfo from a share. The verifier uses
// only the operator list (committee membership check), so a zero CommitteeID
// suffices for tests.
func obftCommitteeInfo(share *ssvtypes.SSVShare) CommitteeInfo {
	ops := make([]spectypes.OperatorID, 0, len(share.Committee))
	for _, m := range share.Committee {
		ops = append(ops, m.Signer)
	}
	return newCommitteeInfo(spectypes.CommitteeID{}, ops, nil)
}

// signOBFTEnvelope wraps a wire-encoded OBFT envelope body in a
// SignedSSVMessage with the given outer signer. The outer RSA signature is
// a fixed 256-byte placeholder — validateOBFTMessage doesn't verify it
// (that's done one layer up); it only checks the inner BLS material.
func signOBFTEnvelope(t testing.TB, msgID spectypes.MessageID, body []byte, signer spectypes.OperatorID) *spectypes.SignedSSVMessage {
	t.Helper()
	return &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{signer},
		Signatures:  [][]byte{make([]byte, 256)},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: ssvmessage.SSVOBFTMsgType,
			MsgID:   msgID,
			Data:    body,
		},
	}
}

// proposerCandidateV builds an OBFT V for the proposer-duty signing
// semantics: `[version | SSZ-marshaled blinded BeaconBlock]`. The validation
// layer's V-side verifier translates V into the block's proposer-domain
// signing root before checking σ_V, so unit tests must use a real blinded
// block in V.
func proposerCandidateV() []byte {
	const version = spec.DataVersionDeneb
	return obftadapter.EncodeCandidate(version, spectestingutils.TestingBlindedBeaconBlockBytesV(version))
}

// ---------------------------------------------------------------------------
// Seed-corpus builders
//
// These produce wire-encoded bytes for use as fuzz seeds (or as inputs to
// adversarial unit tests). Failures here are seed-construction bugs, not
// fuzz finds — we panic on encoder errors.
// ---------------------------------------------------------------------------

// validPhase1BundleBytes returns wire-encoded bytes of a structurally valid
// Phase1Bundle. NOT BLS-valid (LeaderSigma is a fixed placeholder), so feeding
// these to validateOBFTMessage will fail at BLS verify — but they're valid
// seed corpus for the wire decoder.
func validPhase1BundleBytes() []byte {
	b, err := wire.WrapPhase1Bundle(&obftcore.Phase1Bundle{
		ClusterID:   obftTestClusterID,
		OperatorID:  1,
		Height:      12345,
		Layer:       0,
		Value:       []byte("seed-V"),
		LeaderSigma: bytes.Repeat([]byte{0x01}, 96),
	})
	if err != nil {
		panic(fmt.Sprintf("seed: wrap phase-1: %v", err))
	}
	return b
}

// validCommitBytes returns wire-encoded bytes of a structurally valid Commit
// with a few layers, NR partials, and witnesses. Same caveat as
// validPhase1BundleBytes re: BLS-validity — only the wire shape is real.
func validCommitBytes() []byte {
	b, err := wire.WrapCommit(&obftcore.Commit{
		ClusterID:  obftTestClusterID,
		OperatorID: 2,
		Height:     12345,
		Layers: []obftcore.EncryptedLayer{
			{Value: []byte("V0"), Ciphertext: []byte{}},
			{Value: []byte{}, Ciphertext: []byte{}},
			{Value: []byte("V2"), Ciphertext: []byte("ct2")},
			{Value: []byte("V3"), Ciphertext: []byte("ct3")},
		},
		NRPartials: []obftcore.NRPartial{
			{Layer: 1, PartialSig: bytes.Repeat([]byte{0x02}, 96)},
		},
		Witnesses: []obftcore.LeaderSigmaWitness{
			{Layer: 0, Leader: 1, ValueRoot: obftcore.ValueRoot([]byte("V0")), Sigma: bytes.Repeat([]byte{0x03}, 96)},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("seed: wrap commit: %v", err))
	}
	return b
}

// validCertificateBytes returns wire-encoded bytes of a structurally valid
// Certificate.
func validCertificateBytes() []byte {
	b, err := wire.WrapCertificate(&obftcore.Certificate{
		ClusterID: obftTestClusterID,
		Height:    12345,
		Value:     []byte("decided-V"),
		Signature: bytes.Repeat([]byte{0x04}, 96),
	})
	if err != nil {
		panic(fmt.Sprintf("seed: wrap certificate: %v", err))
	}
	return b
}

// blsValidPhase1BundleBytes returns wire-encoded bytes of a Phase1Bundle
// whose LeaderSigma is a real partial BLS sig over the proposer-domain signing
// root of the embedded V. This is admissible at the validation layer
// (passes BLS verify); fuzz mutations of these bytes exercise the
// post-decode validation path most thoroughly.
func blsValidPhase1BundleBytes() []byte {
	ks := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(ks, spectestingutils.TestingValidatorIndex)
	signer := share.Committee[0].Signer

	innerSigner := blsbackend.New(ks.Shares[signer].Serialize())
	vSigner, err := obftadapter.NewProposerSigner(innerSigner, networkconfig.TestNetwork.Beacon)
	if err != nil {
		panic(fmt.Sprintf("seed: proposer signer: %v", err))
	}
	v := obftadapter.EncodeCandidate(spec.DataVersionDeneb, spectestingutils.TestingBlindedBeaconBlockBytesV(spec.DataVersionDeneb))
	sigV, err := vSigner.SignPartial(v)
	if err != nil {
		panic(fmt.Sprintf("seed: sign V: %v", err))
	}
	b, err := wire.WrapPhase1Bundle(&obftcore.Phase1Bundle{
		ClusterID:   obftTestClusterID,
		OperatorID:  obftcore.OperatorID(signer),
		Height:      obftcore.Height(networkconfig.TestNetwork.EstimatedCurrentSlot()),
		Layer:       0,
		Value:       v,
		LeaderSigma: sigV,
	})
	if err != nil {
		panic(fmt.Sprintf("seed: wrap bls phase-1: %v", err))
	}
	return b
}

// ---------------------------------------------------------------------------
// Structural-invariant assertions
//
// These check the bounds the wire decoder is supposed to enforce. Used by
// fuzz tests to detect a decoder regression that lets out-of-bounds values
// escape without an error.
// ---------------------------------------------------------------------------

// assertPhase1BundleInvariants checks the structural bounds for a decoded
// Phase1Bundle.
func assertPhase1BundleInvariants(t testing.TB, b *obftcore.Phase1Bundle) {
	t.Helper()
	if b.Layer < 0 {
		t.Fatalf("Phase1Bundle.Layer = %d (negative)", b.Layer)
	}
	if b.Layer >= wire.MaxLayers {
		t.Fatalf("Phase1Bundle.Layer = %d >= MaxLayers %d", b.Layer, wire.MaxLayers)
	}
	if len(b.Value) > wire.MaxValueSize {
		t.Fatalf("Phase1Bundle.Value len %d > MaxValueSize", len(b.Value))
	}
	if len(b.LeaderSigma) > wire.MaxSignatureSize {
		t.Fatalf("Phase1Bundle.LeaderSigma len %d > MaxSignatureSize", len(b.LeaderSigma))
	}
}

// assertCommitInvariants checks the structural bounds for a decoded Commit.
func assertCommitInvariants(t testing.TB, c *obftcore.Commit) {
	t.Helper()
	if len(c.Layers) > wire.MaxLayers {
		t.Fatalf("Commit.Layers len %d > MaxLayers", len(c.Layers))
	}
	if len(c.NRPartials) > wire.MaxLayers {
		t.Fatalf("Commit.NRPartials len %d > MaxLayers", len(c.NRPartials))
	}
	if len(c.Witnesses) > wire.MaxWitnesses {
		t.Fatalf("Commit.Witnesses len %d > MaxWitnesses", len(c.Witnesses))
	}
	for i, el := range c.Layers {
		if len(el.Value) > wire.MaxValueSize {
			t.Fatalf("Commit.Layers[%d].Value len %d > MaxValueSize", i, len(el.Value))
		}
		if len(el.Ciphertext) > wire.MaxCiphertextSize {
			t.Fatalf("Commit.Layers[%d].Ciphertext len %d > MaxCiphertextSize", i, len(el.Ciphertext))
		}
	}
	for i, p := range c.NRPartials {
		if p.Layer < 0 || p.Layer >= wire.MaxLayers {
			t.Fatalf("Commit.NRPartials[%d].Layer %d out of range", i, p.Layer)
		}
		if len(p.PartialSig) > wire.MaxSignatureSize {
			t.Fatalf("Commit.NRPartials[%d].PartialSig len %d > MaxSignatureSize", i, len(p.PartialSig))
		}
	}
	for i, w := range c.Witnesses {
		if w.Layer < 0 || w.Layer >= wire.MaxLayers {
			t.Fatalf("Commit.Witnesses[%d].Layer %d out of range", i, w.Layer)
		}
		// ValueRoot is fixed 32 bytes — no size-cap check needed.
		if len(w.Sigma) > wire.MaxSignatureSize {
			t.Fatalf("Commit.Witnesses[%d].Sigma len %d > MaxSignatureSize", i, len(w.Sigma))
		}
	}
}

// assertCertificateInvariants checks the structural bounds for a decoded
// Certificate.
func assertCertificateInvariants(t testing.TB, c *obftcore.Certificate) {
	t.Helper()
	if len(c.Value) > wire.MaxValueSize {
		t.Fatalf("Certificate.Value len %d > MaxValueSize", len(c.Value))
	}
	if len(c.Signature) > wire.MaxSignatureSize {
		t.Fatalf("Certificate.Signature len %d > MaxSignatureSize", len(c.Signature))
	}
}

// commitsEqual deep-compares two Commits. Used in roundtrip property
// checks.
func commitsEqual(a, b *obftcore.Commit) bool {
	if a.ClusterID != b.ClusterID ||
		a.OperatorID != b.OperatorID ||
		a.Height != b.Height {
		return false
	}
	if len(a.Layers) != len(b.Layers) {
		return false
	}
	for i := range a.Layers {
		if !bytes.Equal(a.Layers[i].Value, b.Layers[i].Value) ||
			!bytes.Equal(a.Layers[i].Ciphertext, b.Layers[i].Ciphertext) {
			return false
		}
	}
	if len(a.NRPartials) != len(b.NRPartials) {
		return false
	}
	for i := range a.NRPartials {
		if a.NRPartials[i].Layer != b.NRPartials[i].Layer ||
			!bytes.Equal(a.NRPartials[i].PartialSig, b.NRPartials[i].PartialSig) {
			return false
		}
	}
	if len(a.Witnesses) != len(b.Witnesses) {
		return false
	}
	for i := range a.Witnesses {
		if a.Witnesses[i].Layer != b.Witnesses[i].Layer ||
			a.Witnesses[i].Leader != b.Witnesses[i].Leader ||
			a.Witnesses[i].ValueRoot != b.Witnesses[i].ValueRoot ||
			!bytes.Equal(a.Witnesses[i].Sigma, b.Witnesses[i].Sigma) {
			return false
		}
	}
	return true
}
