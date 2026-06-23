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

// PTCDuty is one validator's Payload Timeliness Committee assignment for a slot, as returned
// by the beacon node's /eth/v1/validator/duties/ptc/{epoch} endpoint. JSON-only: PTC duties
// are never SSZ-encoded over the wire.
type PTCDuty struct {
	PubKey         phase0.BLSPubKey
	ValidatorIndex phase0.ValidatorIndex
	Slot           phase0.Slot
}

// ptcDutyJSON is the beacon-API JSON form: pubkey as 0x-hex, uint64 as a decimal string.
type ptcDutyJSON struct {
	PubKey         string `json:"pubkey"`
	ValidatorIndex string `json:"validator_index"`
	Slot           string `json:"slot"`
}

// MarshalJSON implements json.Marshaler.
func (d *PTCDuty) MarshalJSON() ([]byte, error) {
	return json.Marshal(&ptcDutyJSON{
		PubKey:         fmt.Sprintf("%#x", d.PubKey),
		ValidatorIndex: fmt.Sprintf("%d", d.ValidatorIndex),
		Slot:           fmt.Sprintf("%d", d.Slot),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *PTCDuty) UnmarshalJSON(input []byte) error {
	var data ptcDutyJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	pubKey, err := hex.DecodeString(strings.TrimPrefix(data.PubKey, "0x"))
	if err != nil {
		return fmt.Errorf("invalid value for pubkey: %w", err)
	}
	if len(pubKey) != phase0.PublicKeyLength {
		return errors.New("incorrect length for pubkey")
	}
	copy(d.PubKey[:], pubKey)
	validatorIndex, err := strconv.ParseUint(data.ValidatorIndex, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for validator index: %w", err)
	}
	d.ValidatorIndex = phase0.ValidatorIndex(validatorIndex)
	slot, err := strconv.ParseUint(data.Slot, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for slot: %w", err)
	}
	d.Slot = phase0.Slot(slot)
	return nil
}
