package protocol

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"strconv"
)

// StatusCode is the response status code
type StatusCode uint32

const (
	// StatusSuccess means the request went successfully
	StatusSuccess StatusCode = iota
	// StatusNotFound means the desired objects were not found
	StatusNotFound
	// StatusBadRequest means the request was bad
	StatusBadRequest
	// StatusInternalError means that the node experienced an internal error
	StatusInternalError
	// StatusBackoff means we exceeded rate limits for the protocol
	StatusBackoff
)

type SyncMessage struct {
	// Params holds request parameters
	Params []byte
	// Data holds the reaults
	Data []byte // TODO: use structured data
	// Status is the status code of the operation
	Status StatusCode
}

func DecodeSyncMessage(data []byte) (*SyncMessage, error) {
	sm := new(SyncMessage)
	err := sm.UnmarshalJSON(data)
	if err != nil {
		return nil, err
	}
	return sm, nil
}

// MarshalJSON implements json.Marshaler
// the top level values (beside status) will be encoded to hex
func (sm *SyncMessage) MarshalJSON() ([]byte, error) {
	m := make(map[string]string)

	m["params"] = hex.EncodeToString(sm.Params)
	m["data"] = hex.EncodeToString(sm.Data)
	m["status"] = fmt.Sprintf("%d", sm.Status)

	return json.Marshal(m)
}

// UnmarshalJSON implements json.Unmarshaler
func (sm *SyncMessage) UnmarshalJSON(data []byte) error {
	m := make(map[string]string)
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, "could not unmarshal SyncMessage")
	}

	s, err := strconv.Atoi(m["status"])
	if err != nil {
		return errors.Wrap(err, "could not parse status")
	}
	sm.Status = StatusCode(s)

	p, err := hex.DecodeString(m["params"])
	if err != nil {
		return errors.Wrap(err, "could not decode SyncMessage params")
	}
	sm.Params = p
	d, err := hex.DecodeString(m["d"])
	if err != nil {
		return errors.Wrap(err, "could not decode SyncMessage data")
	}
	sm.Data = d

	return nil
}
