package message

import (
	"encoding/json"

	genesisspecqbft "github.com/ssvlabs/ssv-spec-pre-cc/qbft"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
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

// EntryNotFoundError is the error message for when an entry is not found
const EntryNotFoundError = "EntryNotFoundError"

func (sc *StatusCode) String() string {
	switch *sc {
	case StatusUnknown:
		return "Unknown"
	case StatusSuccess:
		return "Success"
	case StatusNotFound:
		return "NotFound"
	case StatusError:
		return "Error"
	case StatusBadRequest:
		return "BadRequest"
	case StatusInternalError:
		return "InternalError"
	case StatusBackoff:
		return "Backoff"
	}
	return ""
}

// SyncParams holds parameters for sync operations
type SyncParams struct {
	// Height of the message, it can hold up to 2 items to specify a range or a single item for specific height
	Height []genesisspecqbft.Height
	// Identifier of the message
	Identifier genesisspectypes.MessageID
}

// SyncMsgType represent the type of sync messages
// TODO: remove after v0
type SyncMsgType int32

const (
	// LastDecidedType is the last decided message type
	LastDecidedType SyncMsgType = iota
	// DecidedHistoryType is the decided history message type
	DecidedHistoryType
)

// SyncMessage is the message being passed in sync operations
type SyncMessage struct {
	// Protocol is the protocol of the message
	// TODO: remove after v0
	Protocol SyncMsgType
	// Params holds request parameters
	Params *SyncParams
	// Data holds the results
	Data []*genesisspecqbft.SignedMessage
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
func (sm *SyncMessage) UpdateResults(err error, results ...*genesisspecqbft.SignedMessage) {
	if err != nil {
		sm.Status = StatusInternalError
	} else if len(results) == 0 || results[0] == nil {
		sm.Status = StatusNotFound
	} else {
		sm.Data = make([]*genesisspecqbft.SignedMessage, len(results))
		copy(sm.Data, results)
		nResults := len(sm.Data)
		// updating params with the actual height of the messages
		sm.Params.Height = []genesisspecqbft.Height{sm.Data[0].Message.Height}
		if nResults > 1 {
			sm.Params.Height = []genesisspecqbft.Height{sm.Data[0].Message.Height, sm.Data[nResults-1].Message.Height}
		}
		sm.Status = StatusSuccess
	}
}
