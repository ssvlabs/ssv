package ibft

import (
	"context"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/instance/eventqueue"
	msgcontinmem "github.com/bloxapp/ssv/ibft/instance/msgcont/inmem"
	"github.com/bloxapp/ssv/ibft/instance/roundtimer"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network/msgqueue"
	"github.com/bloxapp/ssv/utils/dataval/bytesval"
	"github.com/bloxapp/ssv/utils/threadsafe"
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"testing"
	"time"
)

type testSigner struct {
}

func newTestSigner() beacon.KeyManager {
	return &testSigner{}
}

func (s *testSigner) AddShare(shareKey *bls.SecretKey) error {
	return nil
}

func (s *testSigner) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	return nil, nil
}

func (s *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}

func TestChangeRoundTimer(t *testing.T) {
	secretKeys, nodes := GenerateNodes(4)
	instance := &Instance{
		MsgQueue:            msgqueue.New(),
		eventQueue:          eventqueue.New(),
		ChangeRoundMessages: msgcontinmem.New(3, 2),
		PrepareMessages:     msgcontinmem.New(3, 2),
		Config: &proto.InstanceConfig{
			RoundChangeDurationSeconds:   0.2,
			LeaderPreprepareDelaySeconds: 0.1,
		},
		state: &proto.State{
			Round:         threadsafe.Uint64(1),
			Stage:         threadsafe.Int32(int32(proto.RoundState_PrePrepare)),
			Lambda:        threadsafe.BytesS("Lambda"),
			SeqNumber:     threadsafe.Uint64(1),
			PreparedValue: threadsafe.Bytes(nil),
			PreparedRound: threadsafe.Uint64(0),
		},
		ValidatorShare: &storage.Share{
			Committee: nodes,
			NodeID:    1,
			PublicKey: secretKeys[1].GetPublicKey(),
		},
		ValueCheck: bytesval.NewEqualBytes([]byte(time.Now().Weekday().String())),
		Logger:     zaptest.NewLogger(t),
		roundTimer: roundtimer.New(context.Background(), zap.L()),
		signer:     newTestSigner(),
	}
	go instance.startRoundTimerLoop()
	instance.initialized = true
	time.Sleep(time.Millisecond * 200)

	instance.resetRoundTimer()
	time.Sleep(time.Millisecond * 500)
	instance.eventQueue.Pop()()
	require.EqualValues(t, 2, instance.State().Round.Get())
}
