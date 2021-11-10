package operator

import (
	"github.com/bloxapp/ssv/operator/duties"
	v0 "github.com/bloxapp/ssv/operator/forks/v0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

type testDutyCntrl struct {
	c chan uint64
}

func NewTestDutyCntrl() duties.DutyController {
	return &testDutyCntrl{
		c: make(chan uint64),
	}
}

func (d *testDutyCntrl) Start() {
	go func() {
		time.Sleep(time.Millisecond * 150)
		d.c <- 100
	}()
}

func (d *testDutyCntrl) CurrentSlotChan() <-chan uint64 {
	return d.c
}

func TestOperatorNode_listenToCurrentSlot(t *testing.T) {
	node := &operatorNode{
		dutyCtrl: NewTestDutyCntrl(),
		fork:     v0.New(zap.L()),
	}

	node.dutyCtrl.Start()
	go node.listenForCurrentSlot()

	require.EqualValues(t, 0, node.fork.CurrentSlot())

	time.Sleep(time.Millisecond * 200)

	require.EqualValues(t, 100, node.fork.CurrentSlot())
}
