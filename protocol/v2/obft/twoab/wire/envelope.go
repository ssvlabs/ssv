// Package wire provides binary serialization for 2abOBFT messages that
// flow over the network. Independent of the bare-OBFT wire format in
// protocol/v2/obft/base/wire — different ProtocolTag, different
// MessageKind enum, and different message types. Cross-protocol envelopes
// are mutually rejected at decode time (see TestDomainSeparation_*).
//
// The encoding mirrors base/wire's style (length-prefixed fields, big-
// endian integers, version byte) — not SSZ.
package wire

import (
	"fmt"

	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"

	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
)

// EnvelopeVersionV1 re-exports the shared frame version.
const EnvelopeVersionV1 = sharedwire.EnvelopeVersionV1

// MessageKind discriminates the body of a 2abOBFT wire envelope. Independent
// of base/wire's MessageKind — byte values may collide cross-protocol since
// envelopes are namespaced.
type MessageKind byte

const (
	// KindPhase1Bundle indicates the body is an EncodePhase1Bundle-encoded
	// *twoab.Phase1Bundle. Per spec Variant C, the bundle does NOT carry a
	// σ_V threshold partial.
	KindPhase1Bundle MessageKind = 0x01
	// KindVerdict indicates the body is an EncodeVerdict-encoded
	// *twoab.Verdict — Phase-2a op-identity-signed verdict envelope.
	KindVerdict MessageKind = 0x02
	// KindOnion2b indicates the body is an EncodeOnion2b-encoded
	// *twoab.Onion2b — Phase-2b σ-or-NR commit (per-layer onion + NR partials).
	KindOnion2b MessageKind = 0x03
	// KindCertificate indicates the body is an EncodeCertificate-encoded
	// *twoab.Certificate — final-certificate gossip per spec §Phase 3.
	KindCertificate MessageKind = 0x04
)

// String returns a human-readable label for telemetry/logging.
func (k MessageKind) String() string {
	switch k {
	case KindPhase1Bundle:
		return "phase1-bundle"
	case KindVerdict:
		return "phase2a-verdict"
	case KindOnion2b:
		return "phase2b-onion"
	case KindCertificate:
		return "certificate"
	default:
		return fmt.Sprintf("unknown(0x%02x)", byte(k))
	}
}

// Envelope is a parsed wire envelope. Exactly one of the typed fields is
// set, matching Kind.
type Envelope struct {
	Kind         MessageKind
	Phase1Bundle *twoab.Phase1Bundle
	Verdict      *twoab.Verdict
	Onion2b      *twoab.Onion2b
	Certificate  *twoab.Certificate
}

// WrapPhase1Bundle encodes a Phase1Bundle and wraps it in a 2abOBFT wire
// envelope.
func WrapPhase1Bundle(b *twoab.Phase1Bundle) ([]byte, error) {
	body, err := EncodePhase1Bundle(b)
	if err != nil {
		return nil, fmt.Errorf("wire: encode phase-1 bundle: %w", err)
	}
	return wrap(KindPhase1Bundle, body), nil
}

// WrapVerdict encodes a Verdict and wraps it in a 2abOBFT wire envelope.
func WrapVerdict(v *twoab.Verdict) ([]byte, error) {
	body, err := EncodeVerdict(v)
	if err != nil {
		return nil, fmt.Errorf("wire: encode verdict: %w", err)
	}
	return wrap(KindVerdict, body), nil
}

// WrapOnion2b encodes an Onion2b and wraps it in a 2abOBFT wire envelope.
func WrapOnion2b(o *twoab.Onion2b) ([]byte, error) {
	body, err := EncodeOnion2b(o)
	if err != nil {
		return nil, fmt.Errorf("wire: encode onion2b: %w", err)
	}
	return wrap(KindOnion2b, body), nil
}

// WrapCertificate encodes a Certificate and wraps it in a 2abOBFT wire
// envelope.
func WrapCertificate(c *twoab.Certificate) ([]byte, error) {
	body, err := EncodeCertificate(c)
	if err != nil {
		return nil, fmt.Errorf("wire: encode certificate: %w", err)
	}
	return wrap(KindCertificate, body), nil
}

// Unwrap parses a 2abOBFT wire envelope and decodes its body into a typed
// message. Errors on: malformed/truncated envelope, unknown version,
// unknown kind, body decoder error, or ProtocolTag mismatch (which is the
// load-bearing domain separation against bare-OBFT envelopes).
func Unwrap(data []byte) (*Envelope, error) {
	kindByte, body, err := sharedwire.Unframe(data)
	if err != nil {
		return nil, err
	}
	kind := MessageKind(kindByte)
	out := &Envelope{Kind: kind}
	switch kind {
	case KindPhase1Bundle:
		b, err := DecodePhase1Bundle(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode phase-1 bundle body: %w", err)
		}
		out.Phase1Bundle = b
	case KindVerdict:
		v, err := DecodeVerdict(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode verdict body: %w", err)
		}
		out.Verdict = v
	case KindOnion2b:
		o, err := DecodeOnion2b(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode onion2b body: %w", err)
		}
		out.Onion2b = o
	case KindCertificate:
		c, err := DecodeCertificate(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode certificate body: %w", err)
		}
		out.Certificate = c
	default:
		return nil, fmt.Errorf("wire: unknown envelope kind 0x%02x", byte(kind))
	}
	return out, nil
}

func wrap(kind MessageKind, body []byte) []byte {
	return sharedwire.Frame(byte(kind), body)
}
