package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v1/qbft/instance/msgcont"

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
		Identifier:          message.NewIdentifier([]byte("1"), spectypes.BNRoleAttester),
		CurrentInstanceLock: currentInstanceLock,
		ForkLock:            &sync.Mutex{},
	}
	ctrl.SignatureState.setHeight(0)

	tests := []struct {
		name            string
		msgs            []*message.SSVMessage
		expected        []*message.SSVMessage
		expectedQLen    int
		lastHeight      specqbft.Height
		currentInstance instance.Instancer
	}{
		{
			"no_running_instance_late_commit",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(1), 0, ctrl.Identifier, specqbft.CommitMsgType),
			},
			[]*message.SSVMessage{generateConsensusMsg(t, specqbft.Height(1), 0, ctrl.Identifier, specqbft.CommitMsgType)},
			0,
			specqbft.Height(2),
			nil,
		},
		{
			"no_running_instance_too_late_commit",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), 0, ctrl.Identifier, specqbft.CommitMsgType),
			},
			nil,
			0,
			specqbft.Height(2),
			nil,
		},
		{
			"no_running_instance_late_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier),
			},
			[]*message.SSVMessage{generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier)},
			0,
			specqbft.Height(2),
			nil,
		},
		{
			"no_running_instance_sig_current_height",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), 0, ctrl.Identifier, specqbft.RoundChangeMsgType),
				generateConsensusMsg(t, specqbft.Height(1), 0, ctrl.Identifier, specqbft.RoundChangeMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier), // verify priority is right
				generatePostConsensusOrSig(t, message.SSVPostConsensusMsgType, specqbft.Height(1), ctrl.Identifier),
			},
			[]*message.SSVMessage{generatePostConsensusOrSig(t, message.SSVPostConsensusMsgType, specqbft.Height(1), ctrl.Identifier)},
			3,
			specqbft.Height(1),
			nil,
		},
		{
			"by_state_not_started_proposal",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), 1, ctrl.Identifier, specqbft.ProposalMsgType), // verify priority is right
				generateConsensusMsg(t, specqbft.Height(1), 1, ctrl.Identifier, specqbft.ProposalMsgType),
			},
			[]*message.SSVMessage{generateConsensusMsg(t, specqbft.Height(1), specqbft.Round(1), ctrl.Identifier, specqbft.ProposalMsgType)},
			1,
			specqbft.Height(1),
			generateInstance(1, 1, qbft.RoundStateNotStarted),
		},
		{
			"by_state_not_started_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
			},
			1,
			specqbft.Height(0),
			generateInstance(0, 1, qbft.RoundStateNotStarted),
		},
		{
			"by_state_PrePrepare",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),     // verify priority is right
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),
			},
			0,
			specqbft.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"by_state_prepare",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.ProposalMsgType),    // verify priority is right
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
			},
			1,
			specqbft.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrepare),
		},
		{
			"by_state_change_round",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(1), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType), // verify priority is right
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier), // verify priority is right
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType),
			},
			2,
			specqbft.Height(0),
			generateInstance(0, 1, qbft.RoundStateChangeRound),
		},
		{
			"by_state_no_message_pop_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),      // verify priority is right
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType), // verify priority is right
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
			},
			2,
			specqbft.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"by_state_no_message_pop_change_round",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType), // verify priority is right
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType),
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.RoundChangeMsgType),
			},
			1,
			specqbft.Height(0),
			generateInstance(0, 1, qbft.RoundStatePrePrepare),
		},
		{
			"default_late_commit",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(0), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType), // expect previews height commit
			},
			0,
			specqbft.Height(1), // current height
			nil,
		},
		{
			"default_late_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
			},
			0,
			specqbft.Height(1), // current height
			nil,
		},
		{
			"no_running_instance_force_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier),
			},
			2,
			specqbft.Height(1),
			nil,
		},
		{
			"by_state_force_decided",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(0), ctrl.Identifier),
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(2), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(1), ctrl.Identifier),
			},
			2,
			specqbft.Height(1),
			generateInstance(1, 1, qbft.RoundStatePrePrepare),
		},
		{
			"instance_change_round_clean_old_messages",
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(10), specqbft.Round(2), ctrl.Identifier, specqbft.RoundChangeMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(10), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(9), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(9), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(8), specqbft.Round(1), ctrl.Identifier, specqbft.ProposalMsgType), // should be removed
				generateConsensusMsg(t, specqbft.Height(8), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
				generateConsensusMsg(t, specqbft.Height(8), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
				generateConsensusMsg(t, specqbft.Height(7), specqbft.Round(1), ctrl.Identifier, specqbft.ProposalMsgType), // should be removed
				generateConsensusMsg(t, specqbft.Height(7), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
				generateConsensusMsg(t, specqbft.Height(7), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
			},
			[]*message.SSVMessage{
				generateConsensusMsg(t, specqbft.Height(10), specqbft.Round(2), ctrl.Identifier, specqbft.RoundChangeMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(10), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(9), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(9), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
			},
			1, // only "signed_index" for decided in height 9
			specqbft.Height(10),
			generateInstance(10, 1, qbft.RoundStateChangeRound),
		},
		{
			"no_running_instance_change_round_clean_old_messages",
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVPostConsensusMsgType, specqbft.Height(10), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(10), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(9), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(9), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(8), specqbft.Round(1), ctrl.Identifier, specqbft.ProposalMsgType), // should be removed
				generateConsensusMsg(t, specqbft.Height(8), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
				generateConsensusMsg(t, specqbft.Height(8), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
				generateConsensusMsg(t, specqbft.Height(7), specqbft.Round(1), ctrl.Identifier, specqbft.ProposalMsgType), // should be removed
				generateConsensusMsg(t, specqbft.Height(7), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
				generateConsensusMsg(t, specqbft.Height(7), specqbft.Round(1), ctrl.Identifier, specqbft.PrepareMsgType),  // should be removed
			},
			[]*message.SSVMessage{
				generatePostConsensusOrSig(t, message.SSVPostConsensusMsgType, specqbft.Height(10), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(10), ctrl.Identifier),
				generatePostConsensusOrSig(t, message.SSVDecidedMsgType, specqbft.Height(9), ctrl.Identifier),
				generateConsensusMsg(t, specqbft.Height(9), specqbft.Round(1), ctrl.Identifier, specqbft.CommitMsgType),
			},
			1, // only "signed_index" for decided in height 10
			specqbft.Height(10),
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
					case message.SSVDecidedMsgType:
						fallthrough
					case message.SSVConsensusMsgType:
						testSignedMsg := new(specqbft.SignedMessage)
						require.NoError(t, testSignedMsg.Decode(expectedMsg.Data))

						signedMsg := new(specqbft.SignedMessage)
						require.NoError(t, signedMsg.Decode(msg.Data))

						require.Equal(t, testSignedMsg.Message.MsgType, signedMsg.Message.MsgType)
						require.Equal(t, testSignedMsg.Message.Height, signedMsg.Message.Height)
						require.Equal(t, testSignedMsg.Message.Round, signedMsg.Message.Round)
						ctrl.Logger.Debug("msg info",
							zap.Int("type", int(signedMsg.Message.MsgType)),
							zap.Uint64("h", uint64(signedMsg.Message.Height)),
						)
					}
					processed++
					ctrl.Logger.Debug("processed", zap.Int("processed", processed), zap.Int("total", len(test.expected)))
					wg.Done()
					return nil
				})
				wg.Done()
			}()

			wg.Wait()                          // wait until all expected msg's are processed
			time.Sleep(time.Millisecond * 500) // wait for clean old msg's
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

func generateInstance(height specqbft.Height, round specqbft.Round, stage qbft.RoundState) instance.Instancer {
	i := &InstanceMock{state: &qbft.State{
		Height: qbft.NewHeight(height),
		Round:  qbft.NewRound(round),
		Stage:  *atomic.NewInt32(int32(stage))}}
	return i
}

func generatePostConsensusOrSig(t *testing.T, msgType message.MsgType, height specqbft.Height, id message.Identifier) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: msgType,
		ID:      id,
	}

	signedMsg := specqbft.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &specqbft.Message{
			MsgType:    specqbft.CommitMsgType,
			Height:     height,
			Round:      0,
			Identifier: nil,
			Data:       nil,
		},
	}
	data, err := signedMsg.Encode()
	require.NoError(t, err)
	ssvMsg.Data = data
	return ssvMsg
}

func generateConsensusMsg(t *testing.T, height specqbft.Height, round specqbft.Round, id message.Identifier, consensusType specqbft.MessageType) *message.SSVMessage {
	ssvMsg := &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      id,
	}

	signedMsg := specqbft.SignedMessage{
		Signature: nil,
		Signers:   nil,
		Message: &specqbft.Message{
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

func (i *InstanceMock) Containers() map[specqbft.MessageType]msgcont.MessageContainer {
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
func (i *InstanceMock) ForceDecide(msg *specqbft.SignedMessage) {}
func (i *InstanceMock) GetStageChan() chan qbft.RoundState {
	panic("not implemented")
}
func (i *InstanceMock) GetLastChangeRoundMsg() *specqbft.SignedMessage {
	panic("not implemented")
}
func (i *InstanceMock) CommittedAggregatedMsg() (*specqbft.SignedMessage, error) {
	panic("not implemented")
}
func (i *InstanceMock) GetCommittedAggSSVMessage() (message.SSVMessage, error) {
	panic("not implemented")
}
func (i *InstanceMock) ProcessMsg(msg *specqbft.SignedMessage) (bool, error) {
	panic("not implemented")
}
func (i *InstanceMock) ResetRoundTimer() {}
func (i *InstanceMock) BroadcastChangeRound() error {
	panic("not implemented")
}
