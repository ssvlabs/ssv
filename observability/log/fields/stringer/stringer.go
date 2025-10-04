package stringer

import (
	"encoding/hex"
	"strconv"
)

type HexStringer struct {
	Val []byte
}

func (h HexStringer) String() string {
	return hex.EncodeToString(h.Val)
}

type Int64Stringer struct {
	Val int64
}

func (h Int64Stringer) String() string {
	return strconv.FormatInt(h.Val, 10)
}

type Uint64Stringer struct {
	Val uint64
}

func (h Uint64Stringer) String() string {
	return strconv.FormatUint(h.Val, 10)
}

type Float64Stringer struct {
	Val float64
}

func (h Float64Stringer) String() string {
	return strconv.FormatFloat(h.Val, 'f', -1, 64)
}
