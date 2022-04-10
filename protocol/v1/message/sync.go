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

// UpdateResults updates the given sync message with results or potential error
func (sm *SyncMessage) UpdateResults(err error, results ...*SignedMessage) {
	if err != nil {
		sm.Status = StatusInternalError
	} else if len(results) == 0 || results[0] == nil {
		sm.Status = StatusNotFound
	} else {
		sm.Data = make([]*SignedMessage, len(results))
		for i, res := range results {
			sm.Data[i] = res
		}
		nResults := len(sm.Data)
		// updating params with the actual height of the messages
		sm.Params.Height = []Height{sm.Data[0].Message.Height}
		if nResults > 1 {
			sm.Params.Height = []Height{sm.Data[0].Message.Height, sm.Data[nResults-1].Message.Height}
		}
		sm.Status = StatusSuccess
	}
}
