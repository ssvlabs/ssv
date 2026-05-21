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
	// *twoab.Phase1Bundle. The bundle does NOT carry a σ_V threshold partial.
	KindPhase1Bundle MessageKind = 0x01
	// KindValue indicates the body is an EncodeValueMsg-encoded
	// *twoab.ValueMsg — Phase-2a coordination envelope, op has V_0 + host
	// valid (or A1 upgrade from prior KindNoValue).
	KindValue MessageKind = 0x02
	// KindNoValue indicates the body is an EncodeNoValueMsg-encoded
	// *twoab.NoValueMsg — Phase-2a coordination envelope, op has no V_0
	// or host says not-valid.
	KindNoValue MessageKind = 0x03
	// KindCommit indicates the body is an EncodeCommit-encoded
	// *twoab.Commit — Phase-2b binding envelope (Side=Signed or NR) OR
	// Phase-2a NR-direct emission (Side=NRDirect, with L_k>0 LayerEntries).
	KindCommit MessageKind = 0x04
	// KindCertificate indicates the body is an EncodeCertificate-encoded
	// *twoab.Certificate — final-certificate gossip per spec §Phase 3.
	KindCertificate MessageKind = 0x05
)

// String returns a human-readable label for telemetry/logging.
func (k MessageKind) String() string {
	switch k {
	case KindPhase1Bundle:
		return "phase1-bundle"
	case KindValue:
		return "phase2a-value"
	case KindNoValue:
		return "phase2a-novalue"
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
	Phase1Bundle *twoab.Phase1Bundle
	ValueMsg     *twoab.ValueMsg
	NoValueMsg   *twoab.NoValueMsg
	Commit       *twoab.Commit
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

// WrapValueMsg encodes a ValueMsg and wraps it in a 2abOBFT wire envelope.
func WrapValueMsg(v *twoab.ValueMsg) ([]byte, error) {
	body, err := EncodeValueMsg(v)
	if err != nil {
		return nil, fmt.Errorf("wire: encode ValueMsg: %w", err)
	}
	return wrap(KindValue, body), nil
}

// WrapNoValueMsg encodes a NoValueMsg and wraps it in a 2abOBFT wire
// envelope.
func WrapNoValueMsg(nv *twoab.NoValueMsg) ([]byte, error) {
	body, err := EncodeNoValueMsg(nv)
	if err != nil {
		return nil, fmt.Errorf("wire: encode NoValueMsg: %w", err)
	}
	return wrap(KindNoValue, body), nil
}

// WrapCommit encodes a Commit and wraps it in a 2abOBFT wire envelope.
func WrapCommit(c *twoab.Commit) ([]byte, error) {
	body, err := EncodeCommit(c)
	if err != nil {
		return nil, fmt.Errorf("wire: encode Commit: %w", err)
	}
	return wrap(KindCommit, body), nil
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
	case KindValue:
		v, err := DecodeValueMsg(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode ValueMsg body: %w", err)
		}
		out.ValueMsg = v
	case KindNoValue:
		nv, err := DecodeNoValueMsg(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode NoValueMsg body: %w", err)
		}
		out.NoValueMsg = nv
	case KindCommit:
		c, err := DecodeCommit(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode Commit body: %w", err)
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
