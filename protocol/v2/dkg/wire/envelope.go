// Package wire provides the on-wire format for SSV.s OBFT-IBE DKG ceremony
// messages. The DKG ceremony is described in docs/TBFT-DKG-TASKS.md; this
// package covers Phase A2 (wire format) of that plan.
//
// The envelope is a thin discriminated wrapper that lets a single byte
// stream carry any of the four DKG-ceremony message kinds (Exchange, Deal,
// Response, Justification). The intended integration is:
//
//	WrapExchange(e)   → bytes
//	WrapDeal(d)       → bytes
//	WrapResponse(r)   → bytes
//	WrapJustification(j) → bytes
//
// The SSV adapter puts those bytes into SignedSSVMessage.Data and lets the
// existing operator-key signing cover authentication — the envelope itself
// carries no signature field.
//
// Routing fields (ClusterID, Generation, OperatorID) live in each body kind,
// not in the envelope, mirroring the OBFT envelope's design where routing
// keys (Height, OperatorID) live in the inner Onion / NonReceiptAttestation
// payloads. The envelope is a one-byte discriminator + body.
//
// Encoding format of bodies is JSON, with kyber.Point and kyber.Scalar
// fields hex-encoded (matching ssvlabs/ssv-dkg's encoding choice in
// pkgs/wire). The DKG ceremony is once-per-cluster-lifetime, so the
// debuggability of JSON outweighs the bandwidth advantage of a binary
// format. The runtime OBFT path stays binary (see protocol/v2/obft/wire).
//
// On-wire shape:
//
//	[1] envelope version (currently 0x01)
//	[1] kind            (KindExchange=0x01, KindDeal=0x02,
//	                     KindResponse=0x03, KindJustification=0x04)
//	[N] body            (JSON-encoded body for the kind)
package wire

import (
	"errors"
	"fmt"

	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"
)

// EnvelopeVersionV1 re-exports the shared frame version for callers that
// reference it via the DKG-wire package.
const EnvelopeVersionV1 = sharedwire.EnvelopeVersionV1

// MessageKind discriminates the body of a DKG wire envelope.
type MessageKind byte

const (
	// KindExchange indicates the body is an EncodeExchange-encoded
	// *Exchange — the pre-DKG message announcing a participant's
	// kyber-side long-term pubkey for this ceremony.
	KindExchange MessageKind = 0x01
	// KindDeal indicates the body is an EncodeDealBundle-encoded
	// *dkg.DealBundle wrapped in a routing envelope.
	KindDeal MessageKind = 0x02
	// KindResponse indicates the body is an EncodeResponseBundle-encoded
	// *dkg.ResponseBundle wrapped in a routing envelope.
	KindResponse MessageKind = 0x03
	// KindJustification indicates the body is an EncodeJustificationBundle-
	// encoded *dkg.JustificationBundle wrapped in a routing envelope.
	KindJustification MessageKind = 0x04
)

// Envelope is a parsed wire envelope. Exactly one of the typed fields is
// set, matching Kind.
type Envelope struct {
	Kind          MessageKind
	Exchange      *Exchange
	Deal          *DealEnvelope
	Response      *ResponseEnvelope
	Justification *JustificationEnvelope
}

// WrapExchange encodes an Exchange and wraps it in a DKG wire envelope.
func WrapExchange(e *Exchange) ([]byte, error) {
	body, err := EncodeExchange(e)
	if err != nil {
		return nil, fmt.Errorf("wire: encode exchange: %w", err)
	}
	return wrap(KindExchange, body), nil
}

// WrapDeal encodes a DealEnvelope and wraps it in a DKG wire envelope.
func WrapDeal(d *DealEnvelope) ([]byte, error) {
	body, err := EncodeDealEnvelope(d)
	if err != nil {
		return nil, fmt.Errorf("wire: encode deal: %w", err)
	}
	return wrap(KindDeal, body), nil
}

// WrapResponse encodes a ResponseEnvelope and wraps it in a DKG wire envelope.
func WrapResponse(r *ResponseEnvelope) ([]byte, error) {
	body, err := EncodeResponseEnvelope(r)
	if err != nil {
		return nil, fmt.Errorf("wire: encode response: %w", err)
	}
	return wrap(KindResponse, body), nil
}

// WrapJustification encodes a JustificationEnvelope and wraps it in a DKG
// wire envelope.
func WrapJustification(j *JustificationEnvelope) ([]byte, error) {
	body, err := EncodeJustificationEnvelope(j)
	if err != nil {
		return nil, fmt.Errorf("wire: encode justification: %w", err)
	}
	return wrap(KindJustification, body), nil
}

// Unwrap parses a DKG wire envelope and decodes its body into one of the
// typed Exchange / DealEnvelope / ResponseEnvelope / JustificationEnvelope
// fields, set according to Kind.
//
// Errors on: malformed/truncated envelope, unknown version, unknown kind,
// or decoder error from the body. Decoding kyber bundle bodies requires a
// suite (point/scalar deserialization is suite-bound); the suite is
// supplied by the caller — `nil` is acceptable for KindExchange (no
// kyber types in the body).
func Unwrap(data []byte, suite KyberSuite) (*Envelope, error) {
	kindByte, body, err := sharedwire.Unframe(data)
	if err != nil {
		return nil, err
	}
	kind := MessageKind(kindByte)

	out := &Envelope{Kind: kind}
	switch kind {
	case KindExchange:
		e, err := DecodeExchange(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode exchange body: %w", err)
		}
		out.Exchange = e
	case KindDeal:
		if suite == nil {
			return nil, errors.New("wire: decoding deal body requires a non-nil suite")
		}
		d, err := DecodeDealEnvelope(body, suite)
		if err != nil {
			return nil, fmt.Errorf("wire: decode deal body: %w", err)
		}
		out.Deal = d
	case KindResponse:
		r, err := DecodeResponseEnvelope(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode response body: %w", err)
		}
		out.Response = r
	case KindJustification:
		if suite == nil {
			return nil, errors.New("wire: decoding justification body requires a non-nil suite")
		}
		j, err := DecodeJustificationEnvelope(body, suite)
		if err != nil {
			return nil, fmt.Errorf("wire: decode justification body: %w", err)
		}
		out.Justification = j
	default:
		return nil, fmt.Errorf("wire: unknown envelope kind 0x%02x", byte(kind))
	}
	return out, nil
}

func wrap(kind MessageKind, body []byte) []byte {
	return sharedwire.Frame(byte(kind), body)
}
