package controller

import (
	"context"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	qbftspec "github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft"
	"github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/qbft/msgqueue"
	"github.com/bloxapp/ssv/protocol/v1/qbft/pipelines"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
}

func TestConsumeMessages(t *testing.T) {
	q, err := msgqueue.New(
		logex.GetLogger().With(zap.String("who", "msg_q")),
		msgqueue.WithIndexers(msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer()),
	)
	require.NoError(t, err)
	currentInstanceLock := &sync.RWMutex{}
	ctrl := Controller{
		Ctx:                 context.Background(),
		Logger:              logex.GetLogger().With(zap.String("who", "controller")),
		Q:                   q,
		SignatureState:      SignatureState{},
		Identifier:          message.NewIdentifier([]byte("1"), message.RoleTypeAttester),
		CurrentInstanceLock: currentInstanceLock,
		ForkLock:            &sync.Mutex{},
	}
	ctrl.SignatureState.setHeight(0)

	tests := []struct {
		name            string
		msgs            []*message.SSVMessage
		expected        []*message.SSVMessage
		expectedQLen    int
		lastHeight      message.Height
		currentInstance instance.Instancer
	}{
		{
			"no_running_instance_late_commit",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType)},
			0,
			message.Height(2),
			nil,
		},
		{
			"no_running_instance_too_late_commit",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), 0, ctrl.Identifier, message.CommitMsgType),
			},
			nil,
			0,
			message.Height(2),
			nil,
		},
		{
			"no_running_instance_late_decided",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType)},
			0,
			message.Height(2),
			nil,
		},
		{
			"no_running_instance_sig_current_slot",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), 0, ctrl.Identifier, message.RoundChangeMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(1), 0, ctrl.Identifier, message.RoundChangeMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), 0, ctrl.Identifier, message.CommitMsgType),
				generatePartialSignatureMsg(t, message.SSVPostConsensusMsgType, phase0.Slot(1), ctrl.Identifier),
			},
			[]*message.SSVMessage{generatePartialSignatureMsg(t, message.SSVPostConsensusMsgType, phase0.Slot(1), ctrl.Identifier)},
			3,
			message.Height(1),
			nil,
		},
		{
			"by_state_not_started_proposal",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), 1, ctrl.Identifier, message.ProposalMsgType), // verify priority is right
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(1), 1, ctrl.Identifier, message.ProposalMsgType),
			},
			[]*message.SSVMessage{generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(1), message.Round(1), ctrl.Identifier, message.ProposalMsgType)},
			1,
			message.Height(1),
			generateInstance(1, 1, qbft.RoundStateNotStarted),
		},
		{
			"by_state_not_started_decided",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), 1, ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), 1, ctrl.Identifier, message.CommitMsgType),
			},
			1,
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStateNotStarted),
		},
		{
			"by_state_PrePrepare",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.PrepareMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.PrepareMsgType),     // verify priority is right
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.PrepareMsgType),
			},
			0,
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"by_state_prepare",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.ProposalMsgType),    // verify priority is right
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			1,
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrepare),
		},
		{
			"by_state_change_round",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(1), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
			},
			2,
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStateChangeRound),
		},
		{
			"by_state_no_message_pop_decided",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),      // verify priority is right
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			2,
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"by_state_no_message_pop_change_round",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType), // verify priority is right
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.RoundChangeMsgType),
			},
			1,
			message.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"default_late_commit",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType), // expect previews height commit
			},
			0,
			message.Height(1), // current height
			nil,
		},
		{
			"default_late_decided",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			0,
			message.Height(1), // current height
			nil,
		},
		{
			"no_running_instance_force_decided",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(2), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(2), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			2,
			message.Height(1),
			nil,
		},
		{
			"by_state_force_decided",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(2), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(0), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(2), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(1), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			2,
			message.Height(1),
			generateInstance(1, 1, qbft.RoundStatePrePrepare),
		},
		{
			"instance_change_round_clean_old_messages",
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(10), message.Round(2), ctrl.Identifier, message.RoundChangeMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(10), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(8), message.Round(1), ctrl.Identifier, message.ProposalMsgType), // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(8), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(8), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(7), message.Round(1), ctrl.Identifier, message.ProposalMsgType), // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(7), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(7), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
			},
			[]*message.SSVMessage{
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(10), message.Round(2), ctrl.Identifier, message.RoundChangeMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(10), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			1, // only "signed_index" for decided in height 9
			message.Height(10),
			generateInstance(10, 1, qbft.RoundStateChangeRound),
		},
		{
			"no_running_instance_change_round_clean_old_messages",
			[]*message.SSVMessage{
				generatePartialSignatureMsg(t, message.SSVPostConsensusMsgType, phase0.Slot(1), ctrl.Identifier),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(10), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(8), message.Round(1), ctrl.Identifier, message.ProposalMsgType), // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(8), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(8), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(7), message.Round(1), ctrl.Identifier, message.ProposalMsgType), // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(7), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(7), message.Round(1), ctrl.Identifier, message.PrepareMsgType),  // should be removed
			},
			[]*message.SSVMessage{
				generatePartialSignatureMsg(t, message.SSVPostConsensusMsgType, phase0.Slot(1), ctrl.Identifier),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(10), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVDecidedMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
				generateSignedMsg(t, message.SSVConsensusMsgType, message.Height(9), message.Round(1), ctrl.Identifier, message.CommitMsgType),
			},
			1, // only "signed_index" for decided in height 10
			message.Height(10),
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			ctrl.Ctx = ctx
			ctrl.setCurrentInstance(test.currentInstance)
			ctrl.SignatureState = SignatureState{}
			ctrl.SignatureState.setHeight(test.lastHeight)
			ctrl.SignatureState.duty = &beacon.Duty{Slot: 1}

			// populating msg's
			for _, msg := range test.msgs {
				ctrl.Q.Add(msg)
			}

			// set timeout
			go func() {
				time.Sleep(time.Second * 2) // test time out
				if ctx.Err() == nil && test.expected != nil {
					panic("time out")
				}
			}()

			var wg sync.WaitGroup
			wg.Add(len(test.expected))
			processed := 0
			go func() {
				ctrl.StartQueueConsumer(func(msg *message.SSVMessage) error {
					ctrl.Logger.Debug("processing msg", zap.String("type", msg.MsgType.String()))
					if processed == len(test.expected) {
						ctrl.Logger.Debug("processed all expected msg's. ignore msg")
						return nil
					}
					// when done, cancel ctx
					expectedMsg := test.expected[processed]
					require.Equal(t, expectedMsg.MsgType, msg.MsgType)

					switch expectedMsg.MsgType {
					case message.SSVPostConsensusMsgType:
						testSignedMsg := new(ssv.PartialSignatureMessage)
						require.NoError(t, testSignedMsg.Decode(expectedMsg.Data))

						signedMsg := new(ssv.PartialSignatureMessage)
						require.NoError(t, signedMsg.Decode(msg.Data))

						require.Equal(t, testSignedMsg.Slot, signedMsg.Slot)
					case message.SSVDecidedMsgType:
						fallthrough
					case message.SSVConsensusMsgType:
						testSignedMsg := new(message.SignedMessage)
						require.NoError(t, testSignedMsg.Decode(expectedMsg.Data))

						signedMsg := new(message.SignedMessage)
						require.NoError(t, signedMsg.Decode(msg.Data))

						require.Equal(t, testSignedMsg.Message.MsgType, signedMsg.Message.MsgType)
						require.Equal(t, testSignedMsg.Message.Height, signedMsg.Message.Height)
						require.Equal(t, testSignedMsg.Message.Round, signedMsg.Message.Round)
						ctrl.Logger.Debug("msg info", zap.String("type", signedMsg.Message.MsgType.String()), zap.Uint64("h", uint64(signedMsg.Message.Height)))
					}
					processed++
					ctrl.Logger.Debug("processed", zap.Int("processed", processed), zap.Int("total", len(test.expected)))
					wg.Done()
					return nil
				})
				wg.Done()
			}()

			wg.Wait()                          // wait until all expected msg's are processed
			time.Sleep(time.Millisecond * 250) // wait for clean old msg's
			require.Equal(t, test.expectedQLen, ctrl.Q.Len())
			wg.Add(1)
			cancel()
			wg.Wait()                             // wait for queue to cancel
			q.Clean(func(s msgqueue.Index) bool { // make sure queue is empty for next test
				return true
			})
			ctrl.Logger.Debug("done by processed", zap.String("name", test.name))
		})
	}
}

func generateInstance(height message.Height, round message.Round, stage qbft.RoundState) instance.Instancer {
	i := &InstanceMock{state: &qbft.State{
		Height: qbft.NewHeight(height),
		Round:  qbft.NewRound(round),
		Stage:  *atomic.NewInt32(int32(stage))}}
	return i
}

func generatePartialSignatureMsg(t *testing.T, msgType message.MsgType, slot phase0.Slot, id message.Identifier) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: msgType,
		ID:      id,
	}

	psms := ssv.PartialSignatureMessages{
		&ssv.PartialSignatureMessage{
			Slot:             slot,
			PartialSignature: []byte("sig"),
			SigningRoot:      []byte("root"),
			Signers:          nil,
			MetaData:         nil,
		},
	}

	signedMsg := ssv.SignedPartialSignatureMessage{
		Type:      ssv.PostConsensusPartialSig,
		Messages:  psms,
		Signature: []byte("sig"),
		Signers:   nil,
	}

	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

func generateSignedMsg(t *testing.T, msgType message.MsgType, height message.Height, round message.Round, id message.Identifier, consensusType message.ConsensusMessageType) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: msgType,
		ID:      id,
	}

	signedMsg := message.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &message.ConsensusMessage{
			MsgType:    consensusType,
			Height:     height,
			Round:      round,
			Identifier: nil,
			Data:       nil,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

type InstanceMock struct {
	state *qbft.State
}

func (i *InstanceMock) Containers() map[qbftspec.MessageType]msgcont.MessageContainer {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) PrePrepareMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) PrepareMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) CommitMsgValidationPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) CommitMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) DecidedMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) ChangeRoundMsgValidationPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) ChangeRoundMsgPipeline() pipelines.SignedMessagePipeline {
	//TODO implement me
	panic("implement me")
}

func (i *InstanceMock) Init() {}
func (i *InstanceMock) Start(inputValue []byte) error {
	panic("not implemented")
}
func (i *InstanceMock) Stop() {}
func (i *InstanceMock) State() *qbft.State {
	return i.state
}
func (i *InstanceMock) ForceDecide(msg *message.SignedMessage) {}
func (i *InstanceMock) GetStageChan() chan qbft.RoundState {
	panic("not implemented")
}
func (i *InstanceMock) GetLastChangeRoundMsg() *message.SignedMessage {
	panic("not implemented")
}
func (i *InstanceMock) CommittedAggregatedMsg() (*message.SignedMessage, error) {
	panic("not implemented")
}
func (i *InstanceMock) GetCommittedAggSSVMessage() (message.SSVMessage, error) {
	panic("not implemented")
}
func (i *InstanceMock) ProcessMsg(msg *message.SignedMessage) (bool, error) {
	panic("not implemented")
}
func (i *InstanceMock) ResetRoundTimer() {}
func (i *InstanceMock) BroadcastChangeRound() error {
	panic("not implemented")
}
