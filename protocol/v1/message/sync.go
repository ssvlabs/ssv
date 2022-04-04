package message

import (
	"encoding/json"
)

// StatusCode is the response status code
type StatusCode uint32

const (
	// StatusUnknown represents an unknown state
	StatusUnknown StatusCode = iota
	// StatusSuccess means the request went successfully
	StatusSuccess
	// StatusNotFound means the desired objects were not found
	StatusNotFound
	// StatusError means that the node experienced some general error
	StatusError
	// StatusBadRequest means the request was bad
	StatusBadRequest
	// StatusInternalError means that the node experienced an internal error
	StatusInternalError
	// StatusBackoff means we exceeded rate limits for the protocol
	StatusBackoff
)

// SyncParams holds parameters for sync operations
type SyncParams struct {
	// Height of the message, it can hold up to 2 items to specify a range or a single item for specific height
	Height []Height
	// Identifier of the message
	Identifier Identifier
}

// SyncMessage is the message being passed in sync operations
type SyncMessage struct {
	// Params holds request parameters
	Params *SyncParams
	// Data holds the results
	Data []*SignedMessage
	// Status is the status code of the operation
	Status StatusCode
}

// Encode encodes the message
func (sm *SyncMessage) Encode() ([]byte, error) {
	return json.Marshal(sm)
}

// Decode decodes the message
func (sm *SyncMessage) Decode(data []byte) error {
	return json.Unmarshal(data, sm)
}

// UpdateResults updates the given sync message with results
func (sm *SyncMessage) UpdateResults(err error, results ...*SignedMessage) {
	if err != nil {
		sm.Status = StatusInternalError
	} else if len(results) == 0 {
		sm.Status = StatusNotFound
	} else {
		sm.Data = make([]*SignedMessage, len(results))
		for i, res := range results {
			sm.Data[i] = res
		}
		sm.Status = StatusSuccess
	}
}

//// MarshalJSON implements json.Marshaler
//// the top level values (beside status) will be encoded to hex
//func (sm *SyncMessage) MarshalJSON() ([]byte, error) {
//	m := make(map[string]string)
//
//	m["params"] = hex.EncodeToString(sm.Params)
//	m["data"] = hex.EncodeToString(sm.Data)
//	m["status"] = fmt.Sprintf("%d", sm.Status)
//
//	return json.Marshal(m)
//}
//
//// UnmarshalJSON implements json.Unmarshaler
//func (sm *SyncMessage) UnmarshalJSON(data []byte) error {
//	m := make(map[string]string)
//	if err := json.Unmarshal(data, &m); err != nil {
//		return errors.Wrap(err, "could not unmarshal SyncMessage")
//	}
//
//	s, err := strconv.Atoi(m["status"])
//	if err != nil {
//		return errors.Wrap(err, "could not parse status")
//	}
//	sm.Status = StatusCode(s)
//
//	p, err := hex.DecodeString(m["params"])
//	if err != nil {
//		return errors.Wrap(err, "could not decode SyncMessage params")
//	}
//	sm.Params = p
//	d, err := hex.DecodeString(m["d"])
//	if err != nil {
//		return errors.Wrap(err, "could not decode SyncMessage data")
//	}
//	sm.Data = d
//
//	return nil
//}
