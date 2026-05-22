package wire

import (
	"fmt"

	base "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"
)

// Wire envelope for OBFT messages.
//
// The envelope is a thin discriminated wrapper that lets a single byte
// stream carry any of OBFT's three message kinds. The intended integration:
//
//	WrapPhase1Bundle(b)  → bytes
//	WrapCommit(c)        → bytes
//	WrapCertificate(c)   → bytes
//
// The SSV adapter puts those bytes into SignedSSVMessage.Data and lets
// SSV's existing operator-key signing cover authentication. The envelope
// itself does not carry a signature field; the outer SignedSSVMessage's
// signature already authenticates the payload.
//
// On the receive side:
//
//	Unwrap(data) → (kind, parsed message, error)
//
// where the parsed message is one of *base.Phase1Bundle, *base.Commit,
// *base.Certificate matching `Kind`.
//
// Frame layout:
//
//	[1] envelope version (currently 0x01)
//	[1] kind            (KindPhase1Bundle=0x01, KindCommit=0x02,
//	                     KindCertificate=0x04)
//	[N] body bytes      (output of Encode* functions in this package)

// EnvelopeVersionV1 re-exports the shared frame version.
const EnvelopeVersionV1 = sharedwire.EnvelopeVersionV1

// MessageKind discriminates the body of an OBFT wire envelope.
//
// Envelope kinds (this type) are independent of the inner-kind constants in
// wire.go (innerKindPhase1Bundle=0x01, innerKindCommit=0x02,
// innerKindCertificate=0x03) — the inner kind protects the bytes covered by
// the outer SSV signature against type-confusion attacks, the envelope kind
// only dispatches to the right body decoder at Unwrap time. Deliberately
// kept independent so each layer can evolve without coordinated version
// bumps. The envelope-kind value for KindCertificate is 0x04 (skipping 0x03)
// because 0x03 was historically reserved here for a hypothetical future
// kind; the inner-kind side reused 0x03 freely since the two namespaces
// don't collide.
type MessageKind byte

const (
	// KindPhase1Bundle indicates the body is an EncodePhase1Bundle-encoded
	// *base.Phase1Bundle.
	KindPhase1Bundle MessageKind = 0x01
	// KindCommit indicates the body is an EncodeCommit-encoded *base.Commit.
	// Carries the operator's K-layer onion (σ-side) plus their NR partials
	// (NR-side) in a single message emitted at T_commit.
	KindCommit MessageKind = 0x02
	// KindCertificate indicates the body is an EncodeCertificate-encoded
	// *base.Certificate. Value 0x04 (not 0x03) — see the MessageKind type
	// comment for the historical reason this skips 0x03.
	KindCertificate MessageKind = 0x04
)

// String returns a human-readable name for the MessageKind, used in
// logs and error messages. Returns "unknown(0xNN)" for unrecognized
// values. Lowercase-dash casing matches twoab/wire/envelope.go's
// equivalent String method so cross-package log output is uniform.
func (k MessageKind) String() string {
	switch k {
	case KindPhase1Bundle:
		return "phase1-bundle"
	case KindCommit:
		return "commit"
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
	Phase1Bundle *base.Phase1Bundle
	Commit       *base.Commit
	Certificate  *base.Certificate
}

// WrapPhase1Bundle encodes a Phase1Bundle and wraps it in an OBFT wire
// envelope.
func WrapPhase1Bundle(b *base.Phase1Bundle) ([]byte, error) {
	body, err := EncodePhase1Bundle(b)
	if err != nil {
		return nil, fmt.Errorf("wire: encode phase-1 bundle: %w", err)
	}
	return wrap(KindPhase1Bundle, body), nil
}

// WrapCommit encodes a Commit and wraps it in an OBFT wire envelope.
func WrapCommit(c *base.Commit) ([]byte, error) {
	body, err := EncodeCommit(c)
	if err != nil {
		return nil, fmt.Errorf("wire: encode commit: %w", err)
	}
	return wrap(KindCommit, body), nil
}

// WrapCertificate encodes a Certificate and wraps it in an OBFT wire
// envelope.
func WrapCertificate(c *base.Certificate) ([]byte, error) {
	body, err := EncodeCertificate(c)
	if err != nil {
		return nil, fmt.Errorf("wire: encode certificate: %w", err)
	}
	return wrap(KindCertificate, body), nil
}

// Unwrap parses an OBFT wire envelope and decodes its body into a typed
// message.
//
// Errors on: malformed/truncated envelope, unknown version, unknown kind,
// or decoder error from the body.
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
	case KindCommit:
		c, err := DecodeCommit(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode commit body: %w", err)
		}
		out.Commit = c
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
