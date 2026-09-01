package gloas

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// MaxBuilderAuthDataSize is builder-specs' MAX_BUILDER_AUTH_DATA_SIZE: the ByteList limit of BuilderRequestAuth.Data.
const MaxBuilderAuthDataSize = 4096

// BuilderRequestAuth is builder-specs' request-authentication message: Data is the opaque per-builder
// token agreed out of band (defaulting to the UTF-8 bytes of the builder's advertised URL, exactly
// as advertised — never canonicalized, signed exactly as serialized), and Slot is the proposal slot
// the request is authorized for, not the slot at which it is signed or sent. One signed auth covers
// both builder channels (getExecutionPayloadBid and submitBuilderPreferences). Signed under
// DomainBuilderRequestAuth — a genesis-style compute_domain (fork-agnostic); the wire type is
// nonetheless fork-versioned for decoding, so hops carrying the body set Eth-Consensus-Version.
// Variable-size SSZ.
type BuilderRequestAuth struct {
	Data []byte `ssz-max:"4096"`
	Slot phase0.Slot
}

// SignedBuilderRequestAuth is a BuilderRequestAuth plus the validator's signature, carried in builder-API
// request bodies (and forwarded byte-for-byte unchanged by every hop). Variable-size SSZ.
type SignedBuilderRequestAuth struct {
	Message   *BuilderRequestAuth
	Signature phase0.BLSSignature `ssz-size:"96"`
}

// builderRequestAuthJSON is the builder-API JSON form: uint64 as a decimal string, data as 0x-hex, per
// go-eth2-client conventions.
type builderRequestAuthJSON struct {
	Data string `json:"data"`
	Slot string `json:"slot"`
}

// MarshalJSON implements json.Marshaler.
func (r *BuilderRequestAuth) MarshalJSON() ([]byte, error) {
	return json.Marshal(&builderRequestAuthJSON{
		Data: fmt.Sprintf("%#x", r.Data),
		Slot: fmt.Sprintf("%d", r.Slot),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *BuilderRequestAuth) UnmarshalJSON(input []byte) error {
	var data builderRequestAuthJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	b, err := hex.DecodeString(strings.TrimPrefix(data.Data, "0x"))
	if err != nil {
		return fmt.Errorf("invalid value for data: %w", err)
	}
	if len(b) > MaxBuilderAuthDataSize {
		return fmt.Errorf("incorrect length for data: %d bytes exceeds the %d limit", len(b), MaxBuilderAuthDataSize)
	}
	r.Data = b
	slot, err := strconv.ParseUint(data.Slot, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for slot: %w", err)
	}
	r.Slot = phase0.Slot(slot)
	return nil
}

// signedBuilderRequestAuthJSON is the builder-API JSON form of SignedBuilderRequestAuth.
type signedBuilderRequestAuthJSON struct {
	Message   *BuilderRequestAuth `json:"message"`
	Signature string              `json:"signature"`
}

// MarshalJSON implements json.Marshaler.
func (s *SignedBuilderRequestAuth) MarshalJSON() ([]byte, error) {
	return json.Marshal(&signedBuilderRequestAuthJSON{
		Message:   s.Message,
		Signature: fmt.Sprintf("%#x", s.Signature),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SignedBuilderRequestAuth) UnmarshalJSON(input []byte) error {
	var data signedBuilderRequestAuthJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if data.Message == nil {
		return errors.New("message missing")
	}
	s.Message = data.Message
	if err := decodeHexInto(s.Signature[:], data.Signature, "signature"); err != nil {
		return err
	}
	return nil
}
