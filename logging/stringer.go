package logging

import (
	"encoding/hex"
	"strconv"
)

type hexStringer struct {
	val []byte
}

func (h hexStringer) String() string {
	return hex.EncodeToString(h.val)
}

type int64Stringer struct {
	val int64
}

func (h int64Stringer) String() string {
	return strconv.Itoa(int(h.val))
}

type uint64Stringer struct {
	val uint64
}

func (h uint64Stringer) String() string {
	return strconv.FormatUint(h.val, 10)
}
