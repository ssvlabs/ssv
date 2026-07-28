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

// Regenerate with `go generate ./...`. The phase0 --include is resolved from the module graph
// (`go list -m`), so it tracks go-eth2-client across dependency bumps rather than pinning.
//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./request_auth.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0 --objs RequestAuthV1,SignedRequestAuthV1"

// MaxRequestAuthDataSize is builder-specs' MAX_DATA_SIZE: the ByteList limit of RequestAuthV1.Data.
const MaxRequestAuthDataSize = 4096

// RequestAuthV1 is builder-specs' request-authentication message: Data is the opaque per-builder
// token agreed out of band (defaulting to the UTF-8 bytes of the builder's advertised URL, exactly
// as advertised — never canonicalized, signed exactly as serialized), and Slot is the proposal slot
// the request is authorized for, not the slot at which it is signed or sent. One signed auth covers
// both builder channels (getExecutionPayloadBid and submitBuilderPreferences). Signed under
// DomainRequestAuth — genesis-style compute_domain, never fork-versioned. Variable-size SSZ.
type RequestAuthV1 struct {
	Data []byte `ssz-max:"4096"`
	Slot phase0.Slot
}

// SignedRequestAuthV1 is a RequestAuthV1 plus the validator's signature, carried in builder-API
// request bodies (and forwarded byte-for-byte unchanged by every hop). Variable-size SSZ.
type SignedRequestAuthV1 struct {
	Message   *RequestAuthV1
	Signature phase0.BLSSignature `ssz-size:"96"`
}

// requestAuthJSON is the builder-API JSON form: uint64 as a decimal string, data as 0x-hex, per
// go-eth2-client conventions.
type requestAuthJSON struct {
	Data string `json:"data"`
	Slot string `json:"slot"`
}

// MarshalJSON implements json.Marshaler.
func (r *RequestAuthV1) MarshalJSON() ([]byte, error) {
	return json.Marshal(&requestAuthJSON{
		Data: fmt.Sprintf("%#x", r.Data),
		Slot: fmt.Sprintf("%d", r.Slot),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *RequestAuthV1) UnmarshalJSON(input []byte) error {
	var data requestAuthJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	b, err := hex.DecodeString(strings.TrimPrefix(data.Data, "0x"))
	if err != nil {
		return fmt.Errorf("invalid value for data: %w", err)
	}
	if len(b) > MaxRequestAuthDataSize {
		return fmt.Errorf("incorrect length for data: %d bytes exceeds the %d limit", len(b), MaxRequestAuthDataSize)
	}
	r.Data = b
	slot, err := strconv.ParseUint(data.Slot, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for slot: %w", err)
	}
	r.Slot = phase0.Slot(slot)
	return nil
}

// signedRequestAuthJSON is the builder-API JSON form of SignedRequestAuthV1.
type signedRequestAuthJSON struct {
	Message   *RequestAuthV1 `json:"message"`
	Signature string         `json:"signature"`
}

// MarshalJSON implements json.Marshaler.
func (s *SignedRequestAuthV1) MarshalJSON() ([]byte, error) {
	return json.Marshal(&signedRequestAuthJSON{
		Message:   s.Message,
		Signature: fmt.Sprintf("%#x", s.Signature),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SignedRequestAuthV1) UnmarshalJSON(input []byte) error {
	var data signedRequestAuthJSON
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
