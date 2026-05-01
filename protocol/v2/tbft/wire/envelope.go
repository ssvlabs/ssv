package wire

import (
	"fmt"

	"github.com/ssvlabs/ssv/protocol/v2/tbft"
	sharedwire "github.com/ssvlabs/ssv/protocol/v2/wire"
)

// Wire envelope for TBFT messages.
//
// The envelope is a thin discriminated wrapper that lets a single byte
// stream carry either an Onion or a NonReceiptAttestation. The intended
// integration is:
//
//	WrapOnion(onion)   → bytes A
//	WrapNonReceipt(nr) → bytes B
//
//	The SSV adapter then puts those bytes into SignedSSVMessage.Data
//	and lets SSV's existing operator-key signing cover authentication
//	(so the envelope doesn't carry its own signature field — the outer
//	SignedSSVMessage's signature already authenticates the payload).
//
// On the receive side:
//
//	Unwrap(data) → (kind, parsed message, error)
//
// where the parsed message is *tbft.Onion for KindOnion or
// *tbft.NonReceiptAttestation for KindNonReceipt.
//
// Format:
//
//	[1] envelope version (currently 0x01)
//	[1] kind            (KindOnion=0x01 or KindNonReceipt=0x02)
//	[N] body bytes      (output of EncodeOnion / EncodeNonReceipt)

// EnvelopeVersionV1 re-exports the shared frame version for backward
// compatibility with existing TBFT-wire callers.
const EnvelopeVersionV1 = sharedwire.EnvelopeVersionV1

// MessageKind discriminates the body of a TBFT wire envelope.
type MessageKind byte

const (
	// KindOnion indicates the body is an EncodeOnion-encoded *tbft.Onion.
	KindOnion MessageKind = 0x01
	// KindNonReceipt indicates the body is an EncodeNonReceipt-encoded
	// *tbft.NonReceiptAttestation.
	KindNonReceipt MessageKind = 0x02
	// KindCandidate indicates the body is an EncodeCandidate-encoded
	// *tbft.CandidateBroadcast (Phase 1).
	KindCandidate MessageKind = 0x03
)

// Envelope is a parsed wire envelope. Exactly one of the typed fields is
// set, matching Kind.
type Envelope struct {
	Kind       MessageKind
	Onion      *tbft.Onion
	NonReceipt *tbft.NonReceiptAttestation
	Candidate  *tbft.CandidateBroadcast
}

// WrapOnion encodes an Onion and wraps it in a TBFT wire envelope.
func WrapOnion(o *tbft.Onion) ([]byte, error) {
	body, err := EncodeOnion(o)
	if err != nil {
		return nil, fmt.Errorf("wire: encode onion: %w", err)
	}
	return wrap(KindOnion, body), nil
}

// WrapNonReceipt encodes a NonReceiptAttestation and wraps it in a TBFT
// wire envelope.
func WrapNonReceipt(nr *tbft.NonReceiptAttestation) ([]byte, error) {
	body, err := EncodeNonReceipt(nr)
	if err != nil {
		return nil, fmt.Errorf("wire: encode non-receipt: %w", err)
	}
	return wrap(KindNonReceipt, body), nil
}

// WrapCandidate encodes a CandidateBroadcast and wraps it in a TBFT
// wire envelope. This is the Phase-1 message a layer's leader broadcasts
// to distribute their fetched candidate value.
func WrapCandidate(cb *tbft.CandidateBroadcast) ([]byte, error) {
	body, err := EncodeCandidate(cb)
	if err != nil {
		return nil, fmt.Errorf("wire: encode candidate broadcast: %w", err)
	}
	return wrap(KindCandidate, body), nil
}

// Unwrap parses a TBFT wire envelope and decodes its body into a typed
// Onion or NonReceiptAttestation.
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
	case KindOnion:
		o, err := DecodeOnion(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode onion body: %w", err)
		}
		out.Onion = o
	case KindNonReceipt:
		nr, err := DecodeNonReceipt(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode non-receipt body: %w", err)
		}
		out.NonReceipt = nr
	case KindCandidate:
		cb, err := DecodeCandidate(body)
		if err != nil {
			return nil, fmt.Errorf("wire: decode candidate body: %w", err)
		}
		out.Candidate = cb
	default:
		return nil, fmt.Errorf("wire: unknown envelope kind 0x%02x", byte(kind))
	}
	return out, nil
}

func wrap(kind MessageKind, body []byte) []byte {
	return sharedwire.Frame(byte(kind), body)
}
