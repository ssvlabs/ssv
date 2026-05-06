package wire

import (
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"
)

// Wire envelope for OBFT messages.
//
// The envelope is a thin discriminated wrapper that lets a single byte
// stream carry any of OBFT's four message kinds. The intended integration:
//
//	WrapPhase1Bundle(b)  → bytes
//	WrapOnion(o)         → bytes
//	WrapNR(nr)           → bytes
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
// where the parsed message is one of *obft.Phase1Bundle, *obft.Onion,
// *obft.NR, *obft.Certificate matching `Kind`.
//
// Frame layout:
//
//	[1] envelope version (currently 0x01)
//	[1] kind            (KindPhase1Bundle=0x01, KindOnion=0x02, KindNR=0x03,
//	                     KindCertificate=0x04)
//	[N] body bytes      (output of Encode* functions in this package)

// EnvelopeVersionV1 re-exports the shared frame version.
const EnvelopeVersionV1 = sharedwire.EnvelopeVersionV1

// MessageKind discriminates the body of an OBFT wire envelope.
type MessageKind byte

const (
	// KindPhase1Bundle indicates the body is an EncodePhase1Bundle-encoded
	// *obft.Phase1Bundle.
	KindPhase1Bundle MessageKind = 0x01
	// KindOnion indicates the body is an EncodeOnion-encoded *obft.Onion.
	KindOnion MessageKind = 0x02
	// KindNR indicates the body is an EncodeNR-encoded *obft.NR.
	KindNR MessageKind = 0x03
	// KindCertificate indicates the body is an EncodeCertificate-encoded
	// *obft.Certificate.
	KindCertificate MessageKind = 0x04
)

// Envelope is a parsed wire envelope. Exactly one of the typed fields is
// set, matching Kind.
type Envelope struct {
	Kind         MessageKind
	Phase1Bundle *obft.Phase1Bundle
	Onion        *obft.Onion
	NR           *obft.NR
	Certificate  *obft.Certificate
}

// WrapPhase1Bundle encodes a Phase1Bundle and wraps it in an OBFT wire
// envelope.
func WrapPhase1Bundle(b *obft.Phase1Bundle) ([]byte, error) {
	body, err := EncodePhase1Bundle(b)
	if err != nil {
		return nil, fmt.Errorf("wire: encode phase-1 bundle: %w", err)
	}
	return wrap(KindPhase1Bundle, body), nil
}

// WrapOnion encodes an Onion and wraps it in an OBFT wire envelope.
func WrapOnion(o *obft.Onion) ([]byte, error) {
	body, err := EncodeOnion(o)
	if err != nil {
		return nil, fmt.Errorf("wire: encode onion: %w", err)
	}
	return wrap(KindOnion, body), nil
}

// WrapNR encodes an NR and wraps it in an OBFT wire envelope.
func WrapNR(nr *obft.NR) ([]byte, error) {
	body, err := EncodeNR(nr)
	if err != nil {
		return nil, fmt.Errorf("wire: encode NR: %w", err)
	}
	return wrap(KindNR, body), nil
}

// WrapCertificate encodes a Certificate and wraps it in an OBFT wire
// envelope.
func WrapCertificate(c *obft.Certificate) ([]byte, error) {
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
	case KindOnion:
		o, err := DecodeOnion(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode onion body: %w", err)
		}
		out.Onion = o
	case KindNR:
		nr, err := DecodeNR(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode NR body: %w", err)
		}
		out.NR = nr
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
