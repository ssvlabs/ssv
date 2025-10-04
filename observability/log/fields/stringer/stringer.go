package stringer

import (
	"encoding/hex"
	"strconv"
	"time"
)

type HexStringer struct {
	Val []byte
}

func (h HexStringer) String() string {
	return hex.EncodeToString(h.Val)
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

type DynamicDurationMs struct {
	ValFn func() time.Duration
}

func (s DynamicDurationMs) String() string {
	valStr := strconv.FormatInt(s.ValFn().Milliseconds(), 10)
	return valStr + "ms"
}
