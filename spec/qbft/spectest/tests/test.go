package tests

import (
	"github.com/bloxapp/ssv/spec/qbft"
)

type SpecTest struct {
	Name          string
	Pre           *qbft.Instance
	PostRoot      string
	Messages      []*qbft.SignedMessage
	ExpectedError string
}
