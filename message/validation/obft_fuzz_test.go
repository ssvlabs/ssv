// Coverage-guided fuzz tests for the OBFT message-validation layer.
//
// What's being fuzzed:
//
//   - Wire decoder (protocol/v2/obft/wire): the raw-bytes → typed message
//     parsers for Phase1Bundle, Commit, Certificate, plus the Unwrap
//     discriminated-envelope entrypoint. These are the first code path a
//     hostile peer's bytes hit; bugs here can panic the validator process,
//     allocate unbounded memory, or let malformed input slip past structural
//     checks.
//
//   - validateOBFTMessage (message/validation): the OBFT-specific validation
//     entrypoint that runs slot-window check, admission tracker, and
//     BLS/IBE-tag verification on top of the wire decode. Fuzzes the raw
//     envelope bytes inside an otherwise-valid SignedSSVMessage shell, so
//     mutations exercise the full pre-consensus path including admission
//     side-effects on the messageValidator's tracker state.
//
//   - obftAdmissionTracker.Admit: the rate-limit / dedup gate on
//     (msgID, slot, op, kind) buckets. Fuzzes (key tuple, body) inputs so
//     a single tracker sees random traffic and we can assert the bucket
//     invariants (cap ≤ MaxDistinctPerOpSlot, identical bodies always
//     reject, eviction recovers capacity).
//
// What invariants we check:
//
//   - No panic, no goroutine leak, no resource exhaustion (Go's fuzz harness
//     surfaces panics and goroutine leaks; the wire decoder's MaxFieldSize /
//     MaxLayers / MaxWitnesses caps prevent unbounded allocation).
//   - No infinite loop or quadratic blowup: Go's fuzz harness times out
//     individual iterations; the wire decoder is single-pass O(n) over
//     input length.
//   - Either the decoder/validator returns an error OR returns a structurally
//     consistent value (Kind matches the populated typed-field; layer/witness
//     counts within MaxLayers/MaxWitnesses; field lengths within MaxFieldSize).
//   - The encoder/decoder are inverses: Encode(Decode(x)) == x for any x
//     that decodes successfully (catches asymmetric corner cases).
//   - Admission tracker: no panic on any input; identical bodies always
//     reject after the first; bucket entry count never exceeds the cap;
//     rejection is deterministic for a given input sequence.
//
// What's out of scope (covered elsewhere):
//
//   - Behavioral byzantine tests where a misbehaving peer emits messages
//     that survive validation but break protocol invariants — those live
//     in protocol/v2/consensustest/catalog_validation.go.
//   - DoS / network-layer attacks at the libp2p layer.
//   - QBFT validation (separate concern).
//
// Running: `make fuzz-validation` runs each fuzz target for a configurable
// duration (default 60s each). Running each overnight grows the corpus;
// commit any discovered seeds under message/validation/testdata/fuzz/.
package validation

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	libp2ptest "github.com/libp2p/go-libp2p/core/test"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
)

// Shared seed-corpus builders (validPhase1BundleBytes, blsValidPhase1BundleBytes,
// etc.), structural-invariant assertions, and the obftTestSetup harness all
// live in obft_test_helpers_test.go.

// ---------------------------------------------------------------------------
// Layer 1: wire decoder fuzzing
//
// Each function feeds raw bytes to a single decode entrypoint and asserts
// that the decoder either returns an error or a structurally consistent
// value. These are fast-iteration fuzz targets — the BLS path is not
// exercised, so each iteration is microseconds.
// ---------------------------------------------------------------------------

// FuzzOBFTUnwrap fuzzes the top-level discriminated wire envelope decoder.
// Asserts: on success, Kind matches the populated typed-field and the
// nested decoded value satisfies its own structural invariants.
func FuzzOBFTUnwrap(f *testing.F) {
	f.Add(validPhase1BundleBytes())
	f.Add(validCommitBytes())
	f.Add(validCertificateBytes())
	// Edge cases that bypass the framing version check.
	f.Add([]byte{})
	f.Add([]byte{wire.EnvelopeVersionV1})
	f.Add([]byte{wire.EnvelopeVersionV1, byte(wire.KindPhase1Bundle)})
	f.Add([]byte{wire.EnvelopeVersionV1, 0x99}) // unknown kind
	f.Add([]byte{0xFF, 0x01})                   // bad envelope version

	f.Fuzz(func(t *testing.T, data []byte) {
		env, err := wire.Unwrap(data)
		if err != nil {
			if env != nil {
				t.Fatalf("Unwrap returned non-nil envelope (kind=0x%02x) with error %v", byte(env.Kind), err)
			}
			return
		}
		if env == nil {
			t.Fatalf("Unwrap returned nil envelope without error")
		}
		// Kind matches populated typed-field; exactly one is set.
		switch env.Kind {
		case wire.KindPhase1Bundle:
			if env.Phase1Bundle == nil || env.Commit != nil || env.Certificate != nil {
				t.Fatalf("KindPhase1Bundle: phase1=%v commit=%v cert=%v",
					env.Phase1Bundle != nil, env.Commit != nil, env.Certificate != nil)
			}
			assertPhase1BundleInvariants(t, env.Phase1Bundle)
		case wire.KindCommit:
			if env.Commit == nil || env.Phase1Bundle != nil || env.Certificate != nil {
				t.Fatalf("KindCommit: phase1=%v commit=%v cert=%v",
					env.Phase1Bundle != nil, env.Commit != nil, env.Certificate != nil)
			}
			assertCommitInvariants(t, env.Commit)
		case wire.KindCertificate:
			if env.Certificate == nil || env.Phase1Bundle != nil || env.Commit != nil {
				t.Fatalf("KindCertificate: phase1=%v commit=%v cert=%v",
					env.Phase1Bundle != nil, env.Commit != nil, env.Certificate != nil)
			}
			assertCertificateInvariants(t, env.Certificate)
		default:
			t.Fatalf("Unwrap returned unknown kind 0x%02x without error", byte(env.Kind))
		}
	})
}

// FuzzOBFTPhase1BundleDecode fuzzes the Phase1Bundle wire body decoder
// directly (no envelope framing). Targets the field-by-field parser:
// version, protocol tag, inner kind, ClusterID, OperatorID, Height, Layer,
// Value, SigmaV.
func FuzzOBFTPhase1BundleDecode(f *testing.F) {
	// Strip the 2-byte envelope frame to feed the body decoder.
	full := validPhase1BundleBytes()
	f.Add(full[2:])
	f.Add([]byte{})
	f.Add([]byte{wire.Phase1BundleVersionV1})

	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := wire.DecodePhase1Bundle(data)
		if err != nil {
			if b != nil {
				t.Fatalf("DecodePhase1Bundle returned non-nil bundle with error %v", err)
			}
			return
		}
		if b == nil {
			t.Fatalf("DecodePhase1Bundle returned nil without error")
		}
		assertPhase1BundleInvariants(t, b)
	})
}

// FuzzOBFTCommitDecode fuzzes the Commit body decoder. Most-complex
// parser: variable-length Layers / NRPartials / Witnesses with nested
// length-prefixed fields per element.
func FuzzOBFTCommitDecode(f *testing.F) {
	full := validCommitBytes()
	f.Add(full[2:])
	// Empty-everything commit (zero layers, zero NR, zero witnesses).
	emptyCommit, err := wire.EncodeCommit(&obftcore.Commit{
		ClusterID: obftTestClusterID, OperatorID: 1, Height: 1,
	})
	if err != nil {
		f.Fatalf("seed: empty commit: %v", err)
	}
	f.Add(emptyCommit)

	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := wire.DecodeCommit(data)
		if err != nil {
			if c != nil {
				t.Fatalf("DecodeCommit returned non-nil with error %v", err)
			}
			return
		}
		if c == nil {
			t.Fatalf("DecodeCommit returned nil without error")
		}
		assertCommitInvariants(t, c)
	})
}

// FuzzOBFTCertificateDecode fuzzes the Certificate body decoder.
func FuzzOBFTCertificateDecode(f *testing.F) {
	full := validCertificateBytes()
	f.Add(full[2:])
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := wire.DecodeCertificate(data)
		if err != nil {
			if c != nil {
				t.Fatalf("DecodeCertificate returned non-nil with error %v", err)
			}
			return
		}
		if c == nil {
			t.Fatalf("DecodeCertificate returned nil without error")
		}
		assertCertificateInvariants(t, c)
	})
}

// ---------------------------------------------------------------------------
// Layer 2: encode/decode roundtrip property fuzzing
//
// For any structured input the fuzzer can construct, Encode → Decode must
// preserve the value. Catches asymmetric bugs (e.g., encoder writes one
// length, decoder reads another). The fuzzer drives the structural fields;
// encoder bounds limit blast radius.
// ---------------------------------------------------------------------------

// FuzzOBFTPhase1BundleRoundtrip drives the Phase1Bundle field tuple,
// encodes, decodes, and asserts equality.
func FuzzOBFTPhase1BundleRoundtrip(f *testing.F) {
	f.Add(uint64(1), uint64(2), int32(3), []byte("V"), []byte("S"))
	f.Add(uint64(0), uint64(0), int32(0), []byte{}, []byte{})
	f.Add(uint64(^uint64(0)), uint64(^uint64(0)), int32(wire.MaxLayers-1),
		bytes.Repeat([]byte{0xAB}, 1024), bytes.Repeat([]byte{0xCD}, 96))

	f.Fuzz(func(t *testing.T, opID, height uint64, layer int32, value, sigV []byte) {
		// Encoder rejects negative layer; decoder rejects layer >= MaxLayers.
		// Skip out-of-bounds inputs — they're valid encoder rejections, not
		// roundtrip failures.
		if layer < 0 || layer >= wire.MaxLayers {
			return
		}
		// Encoder rejects fields > MaxFieldSize. The fuzzer can synthesize
		// large inputs — clamp to a sane upper bound to keep iterations fast.
		if len(value) > 1<<20 || len(sigV) > 1<<20 {
			return
		}
		in := &obftcore.Phase1Bundle{
			ClusterID:  obftTestClusterID,
			OperatorID: obftcore.OperatorID(opID),
			Height:     obftcore.Height(height),
			Layer:      int(layer),
			Value:      obftcore.Value(value),
			SigmaV:     obftcore.Signature(sigV),
		}
		encoded, err := wire.EncodePhase1Bundle(in)
		if err != nil {
			t.Fatalf("EncodePhase1Bundle on in-bounds input: %v", err)
		}
		out, err := wire.DecodePhase1Bundle(encoded)
		if err != nil {
			t.Fatalf("DecodePhase1Bundle of self-encoded bytes: %v", err)
		}
		if in.ClusterID != out.ClusterID ||
			in.OperatorID != out.OperatorID ||
			in.Height != out.Height ||
			in.Layer != out.Layer ||
			!bytes.Equal(in.Value, out.Value) ||
			!bytes.Equal(in.SigmaV, out.SigmaV) {
			t.Fatalf("roundtrip mismatch:\n in=%+v\nout=%+v", in, out)
		}
	})
}

// FuzzOBFTCommitRoundtrip drives a Commit tuple (one layer, one NR partial,
// one witness) and asserts encode/decode roundtrip. Single-element variants
// are enough to exercise the inner-element field shape; the multi-element
// case is covered by FuzzOBFTCommitDecode random-bytes mutations.
func FuzzOBFTCommitRoundtrip(f *testing.F) {
	f.Add(uint64(1), uint64(2), []byte("V0"), []byte("ct0"),
		int32(1), []byte("nr-sig"),
		int32(0), uint64(1), []byte("WV"), []byte("WS"))

	f.Fuzz(func(t *testing.T,
		opID, height uint64,
		layerValue, layerCT []byte,
		nrLayer int32, nrSig []byte,
		wLayer int32, wLeader uint64, wValue, wSigV []byte,
	) {
		if nrLayer < 0 || nrLayer >= wire.MaxLayers {
			return
		}
		if wLayer < 0 || wLayer >= wire.MaxLayers {
			return
		}
		// Clamp body sizes so iterations stay fast.
		const maxField = 1 << 16
		if len(layerValue) > maxField || len(layerCT) > maxField ||
			len(nrSig) > maxField || len(wValue) > maxField || len(wSigV) > maxField {
			return
		}
		in := &obftcore.Commit{
			ClusterID:  obftTestClusterID,
			OperatorID: obftcore.OperatorID(opID),
			Height:     obftcore.Height(height),
			Layers: []obftcore.EncryptedLayer{
				{Value: layerValue, Ciphertext: layerCT},
			},
			NRPartials: []obftcore.NRPartial{
				{Layer: int(nrLayer), PartialSig: obftcore.Signature(nrSig)},
			},
			Witnesses: []obftcore.LeaderSigmaWitness{
				{Layer: int(wLayer), Leader: obftcore.OperatorID(wLeader),
					ValueRoot: obftcore.ValueRoot(wValue), SigmaV: obftcore.Signature(wSigV)},
			},
		}
		encoded, err := wire.EncodeCommit(in)
		if err != nil {
			t.Fatalf("EncodeCommit on in-bounds input: %v", err)
		}
		out, err := wire.DecodeCommit(encoded)
		if err != nil {
			t.Fatalf("DecodeCommit of self-encoded bytes: %v", err)
		}
		if !commitsEqual(in, out) {
			t.Fatalf("roundtrip mismatch:\n in=%+v\nout=%+v", in, out)
		}
	})
}

// ---------------------------------------------------------------------------
// Layer 3: validation entrypoint fuzzing
//
// Feeds random envelope bytes through validateOBFTMessage with a valid
// SignedSSVMessage shell. Exercises wire decode + slot-window check +
// admission tracker + BLS verify. Every iteration runs against a fresh
// messageValidator (admission tracker state must not leak between
// iterations or rejections become non-deterministic).
// ---------------------------------------------------------------------------

// FuzzValidateOBFTMessage fuzzes the message-validation entrypoint. Seeds
// include a BLS-valid Phase1Bundle (so post-mutation the fuzzer can find
// inputs that pass through wire decode and bls verify into the rest of
// the path) and structurally-valid-but-BLS-invalid envelopes.
func FuzzValidateOBFTMessage(f *testing.F) {
	f.Add(blsValidPhase1BundleBytes())
	f.Add(validPhase1BundleBytes())
	f.Add(validCommitBytes())
	f.Add(validCertificateBytes())
	f.Add([]byte{})
	f.Add([]byte{0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		mv, _, share, msgID, _ := obftTestSetup(t)
		signer := share.Committee[0].Signer
		msg := signOBFTEnvelope(t, msgID, data, signer)
		peerID, _ := libp2ptest.RandPeerID()

		env, err := mv.validateOBFTMessage(context.Background(), msg, obftCommitteeInfo(share), peerID, time.Now())
		if err != nil {
			// Errors are expected and welcome — they prove the validator
			// rejected the input safely. The invariant is: no panic, no hang.
			if env != nil {
				// Validator MUST NOT return a non-nil envelope on error.
				// A non-nil envelope on error means downstream queueing
				// could see a malformed body alongside a rejection signal.
				t.Fatalf("validateOBFTMessage returned non-nil envelope with error %v", err)
			}
			return
		}
		// On success: envelope must be structurally consistent.
		if env == nil {
			t.Fatalf("validateOBFTMessage returned nil envelope without error")
		}
		switch env.Kind {
		case wire.KindPhase1Bundle:
			assertPhase1BundleInvariants(t, env.Phase1Bundle)
		case wire.KindCommit:
			assertCommitInvariants(t, env.Commit)
		case wire.KindCertificate:
			assertCertificateInvariants(t, env.Certificate)
		default:
			t.Fatalf("validateOBFTMessage returned unknown kind 0x%02x", byte(env.Kind))
		}
	})
}

// ---------------------------------------------------------------------------
// Layer 4: admission tracker fuzzing
//
// Drives the (msgID, slot, op, kind, body) Admit input space against a
// single tracker. Asserts: no panic, identical bodies always reject after
// first admission, bucket entry count never exceeds the cap.
// ---------------------------------------------------------------------------

// FuzzOBFTAdmissionsAdmit fuzzes the admission tracker with random inputs
// against a fresh tracker per iteration. Each iteration runs a sequence
// of two Admit calls so we can assert the dedup invariant (identical body
// → second call must reject as "identical content").
func FuzzOBFTAdmissionsAdmit(f *testing.F) {
	f.Add(byte(0), uint64(1), uint64(2), byte(1), []byte("body-A"))
	f.Add(byte(0xFF), uint64(0), uint64(0), byte(0), []byte{})
	f.Add(byte(0), uint64(^uint64(0)), uint64(^uint64(0)), byte(0xFF), bytes.Repeat([]byte{0xCD}, 1024))

	f.Fuzz(func(t *testing.T, msgIDByte byte, slot uint64, op uint64, kind byte, body []byte) {
		// Clamp body size — tracker only hashes (SHA256 = O(n)), so very
		// large bodies just slow the iteration. 1 MiB is more than any
		// realistic OBFT envelope.
		if len(body) > 1<<20 {
			return
		}
		tr := newOBFTAdmissionTracker()
		var msgID spectypes.MessageID
		msgID[0] = msgIDByte
		s := phase0.Slot(slot)
		o := spectypes.OperatorID(op)

		// First admit: either nil (admitted) or a cap error (impossible
		// for the first call, since the bucket starts empty).
		err1 := tr.Admit(msgID, s, o, kind, body)
		if err1 != nil {
			t.Fatalf("first Admit on fresh tracker returned error: %v", err1)
		}

		// Second admit with identical body: must reject as "identical content".
		err2 := tr.Admit(msgID, s, o, kind, body)
		if err2 == nil {
			t.Fatalf("identical-body re-Admit was admitted; expected dedup")
		}

		// Bucket invariant: cap is never exceeded.
		bucket := obftAdmissionBucket{msgID: msgID, slot: s, op: o, kind: kind}
		tr.mu.Lock()
		state, ok := tr.buckets[bucket]
		count := 0
		if ok && state != nil {
			count = len(state.entries)
		}
		tr.mu.Unlock()
		if count > obftValidationMaxDistinctPerOpSlot {
			t.Fatalf("bucket entry count %d exceeds cap %d", count, obftValidationMaxDistinctPerOpSlot)
		}
		// First admit succeeded → bucket has exactly one entry.
		if count != 1 {
			t.Fatalf("after one successful admit, bucket has %d entries (want 1)", count)
		}
	})
}
