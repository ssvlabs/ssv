package eth_test

import (
	"github.com/bloxapp/ssv/eth/simulator"
	"github.com/bloxapp/ssv/eth/simulator/simcontract"
	"github.com/bloxapp/ssv/operator/storage"
	"testing"
)

type commonTestInput struct {
	t             *testing.T
	sim           *simulator.SimulatedBackend
	boundContract *simcontract.Simcontract
	blockNum      *uint64
	nodeStorage   storage.Storage
	doInOneBlock  bool
}

func NewCommonTestInput(
	t *testing.T,
	sim *simulator.SimulatedBackend,
	boundContract *simcontract.Simcontract,
	blockNum *uint64,
	nodeStorage storage.Storage,
	doInOneBlock bool,
) *commonTestInput {
	return &commonTestInput{
		t:             t,
		sim:           sim,
		boundContract: boundContract,
		blockNum:      blockNum,
		nodeStorage:   nodeStorage,
		doInOneBlock:  doInOneBlock,
	}
}
