package api

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

type Hex []byte

func (h Hex) MarshalJSON() ([]byte, error) {
	return []byte("\"" + hex.EncodeToString(h) + "\""), nil
}

func (h *Hex) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errors.New("invalid hex string")
	}
	b, err := hex.DecodeString(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*h = b
	return nil
}

func (h *Hex) Bind(value string) error {
	if value == "" {
		return nil
	}
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
