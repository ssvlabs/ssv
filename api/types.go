package api

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/message"
)

type Hex []byte

func (h Hex) MarshalJSON() ([]byte, error) {
	return []byte("\"" + hex.EncodeToString(h) + "\""), nil
}

func (h *Hex) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errors.New("invalid hex string")
	}

	str := string(data[1 : len(data)-1])

	return h.Bind(str)
}

func (h *Hex) Bind(value string) error {
	if value == "" {
		*h = Hex{}
		return nil
	}

	value = strings.TrimPrefix(value, "0x")
	b, err := hex.DecodeString(value)

	if err != nil {
		return err
	}

	*h = b

	return nil
}

type HexSlice []Hex

func (hs *HexSlice) Bind(value string) error {
	if value == "" {
		return nil
	}
	for _, s := range strings.Split(value, ",") {
		var h Hex
		err := h.Bind(s)
		if err != nil {
			return err
		}
		*hs = append(*hs, h)
	}
	return nil
}

type Uint64Slice []uint64

func (us *Uint64Slice) Bind(value string) error {
	if value == "" {
		return nil
	}
	for _, s := range strings.Split(value, ",") {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		*us = append(*us, n)
	}
	return nil
}

type Role spectypes.BeaconRole

func (r *Role) Bind(value string) error {
	role, err := message.BeaconRoleFromString(value)
	if err != nil {
		return err
	}
	*r = Role(role)
	return nil
}

func (r Role) MarshalJSON() ([]byte, error) {
	return []byte(`"` + spectypes.BeaconRole(r).String() + `"`), nil
}

func (r *Role) UnmarshalJSON(data []byte) error {
	var role string
	err := json.Unmarshal(data, &role)
	if err != nil {
		return err
	}
	return r.Bind(role)
}

type RoleSlice []Role

func (rs *RoleSlice) Bind(value string) error {
	if value == "" {
		return nil
	}
	for _, s := range strings.Split(value, ",") {
		var r Role
		err := r.Bind(s)
		if err != nil {
			return err
		}
		*rs = append(*rs, r)
	}
	return nil
}
