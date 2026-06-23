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

//go:generate sh -c "go tool -modfile=../../../../tool.mod sszgen -path ./payload_attestation.go --include $(go list -m -f '{{.Dir}}' github.com/attestantio/go-eth2-client)/spec/phase0 --objs PayloadAttestationData,PayloadAttestationMessage"

// PayloadAttestationData is the Gloas (ePBS) datum a PTC member attests to: whether the
// execution payload for BeaconBlockRoot at Slot was present, and its blobs available.
// Signed under DomainPTCAttester. Fixed 42-byte SSZ.
type PayloadAttestationData struct {
	BeaconBlockRoot   phase0.Root `ssz-size:"32"`
	Slot              phase0.Slot
	PayloadPresent    bool
	BlobDataAvailable bool
}

// PayloadAttestationMessage is one PTC member's signed PayloadAttestationData, submitted
// to the beacon node's payload_attestations pool. Fixed 146-byte SSZ.
type PayloadAttestationMessage struct {
	ValidatorIndex phase0.ValidatorIndex
	Data           *PayloadAttestationData
	Signature      phase0.BLSSignature `ssz-size:"96"`
}

// payloadAttestationDataJSON is the beacon-API JSON form: uint64 as a decimal string,
// roots as 0x-hex, per go-eth2-client conventions.
type payloadAttestationDataJSON struct {
	BeaconBlockRoot   string `json:"beacon_block_root"`
	Slot              string `json:"slot"`
	PayloadPresent    bool   `json:"payload_present"`
	BlobDataAvailable bool   `json:"blob_data_available"`
}

// MarshalJSON implements json.Marshaler.
func (p *PayloadAttestationData) MarshalJSON() ([]byte, error) {
	return json.Marshal(&payloadAttestationDataJSON{
		BeaconBlockRoot:   fmt.Sprintf("%#x", p.BeaconBlockRoot),
		Slot:              fmt.Sprintf("%d", p.Slot),
		PayloadPresent:    p.PayloadPresent,
		BlobDataAvailable: p.BlobDataAvailable,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *PayloadAttestationData) UnmarshalJSON(input []byte) error {
	var data payloadAttestationDataJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	root, err := hex.DecodeString(strings.TrimPrefix(data.BeaconBlockRoot, "0x"))
	if err != nil {
		return fmt.Errorf("invalid value for beacon block root: %w", err)
	}
	if len(root) != phase0.RootLength {
		return errors.New("incorrect length for beacon block root")
	}
	copy(p.BeaconBlockRoot[:], root)
	slot, err := strconv.ParseUint(data.Slot, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for slot: %w", err)
	}
	p.Slot = phase0.Slot(slot)
	p.PayloadPresent = data.PayloadPresent
	p.BlobDataAvailable = data.BlobDataAvailable
	return nil
}

// payloadAttestationMessageJSON is the beacon-API JSON form of PayloadAttestationMessage.
type payloadAttestationMessageJSON struct {
	ValidatorIndex string                  `json:"validator_index"`
	Data           *PayloadAttestationData `json:"data"`
	Signature      string                  `json:"signature"`
}

// MarshalJSON implements json.Marshaler.
func (p *PayloadAttestationMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(&payloadAttestationMessageJSON{
		ValidatorIndex: fmt.Sprintf("%d", p.ValidatorIndex),
		Data:           p.Data,
		Signature:      fmt.Sprintf("%#x", p.Signature),
	})
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *PayloadAttestationMessage) UnmarshalJSON(input []byte) error {
	var data payloadAttestationMessageJSON
	if err := json.Unmarshal(input, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	validatorIndex, err := strconv.ParseUint(data.ValidatorIndex, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid value for validator index: %w", err)
	}
	p.ValidatorIndex = phase0.ValidatorIndex(validatorIndex)
	if data.Data == nil {
		return errors.New("data missing")
	}
	p.Data = data.Data
	signature, err := hex.DecodeString(strings.TrimPrefix(data.Signature, "0x"))
	if err != nil {
		return fmt.Errorf("invalid value for signature: %w", err)
	}
	if len(signature) != phase0.SignatureLength {
		return errors.New("incorrect length for signature")
	}
	copy(p.Signature[:], signature)
	return nil
}
