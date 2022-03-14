package protocol

import (
	"encoding/hex"
	"encoding/json"
	"github.com/pkg/errors"
)

// MarshalJSON implements json.Marshaler
// all the top level values will be encoded to hex
func (msg *SSVMessage) MarshalJSON() ([]byte, error) {
	m := make(map[string]string)

	d, err := json.Marshal(msg.MsgType)
	if err != nil {
		return nil, errors.Wrap(err, "MsgType marshaling failed")
	}
	m["type"] = hex.EncodeToString(d)

	if msg.ID != nil {
		m["id"] = hex.EncodeToString(msg.ID)
	}

	if msg.Data != nil {
		d, err := json.Marshal(msg.Data)
		if err != nil {
			return nil, errors.Wrap(err, "Data marshaling failed")
		}
		m["Data"] = hex.EncodeToString(d)
	}
	return json.Marshal(m)
}

// UnmarshalJSON implements json.Unmarshaler
func (msg *SSVMessage) UnmarshalJSON(data []byte) error {
	m := make(map[string]string)
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.Wrap(err, "could not unmarshal SSVMessage")
	}

	d, err := hex.DecodeString(m["type"])
	if err != nil {
		return errors.Wrap(err, "SSVMessage decode string failed")
	}
	if err := json.Unmarshal(d, &msg.MsgType); err != nil {
		return errors.Wrap(err, "could not unmarshal MsgType")
	}

	if val, ok := m["id"]; ok {
		d, err := hex.DecodeString(val)
		if err != nil {
			return errors.Wrap(err, "msg id decode string failed")
		}
		msg.ID = d
	}

	if val, ok := m["Data"]; ok {
		msg.Data = make([]byte, 0)
		d, err := hex.DecodeString(val)
		if err != nil {
			return errors.Wrap(err, "Data decode string failed")
		}
		if err := json.Unmarshal(d, &msg.Data); err != nil {
			msg.Data = nil
			return errors.Wrap(err, "could not unmarshal Data")
		}
	}
	return nil
}
