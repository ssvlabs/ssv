package controller

import (
	"context"
	"testing"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qbftStorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	forksfactory "github.com/bloxapp/ssv/protocol/v1/qbft/controller/forks/factory"
	testing2 "github.com/bloxapp/ssv/protocol/v1/testing"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func init() {
	logex.Build("test", zap.DebugLevel, nil)
}

func TestReadModeChangeRound(t *testing.T) {
	logger := logex.GetLogger()
	cfg := basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Ctx:    context.Background(),
	}
	db, err := storage.GetStorageFactory(cfg)
	require.NoError(t, err)
	changeRoundStorage := qbftStorage.New(db, logger, spectypes.BNRoleAttester.String(), forksprotocol.GenesisForkVersion)

	uids := []spectypes.OperatorID{spectypes.OperatorID(1)}
	secretKeys, _ := testing2.GenerateBLSKeys(uids...)

	ctrl := Controller{
		Ctx:    context.Background(),
		Logger: logger,
		ValidatorShare: &beacon.Share{
			NodeID: 1,
			//PublicKey:    secretKeys[0].GetPublicKey(),
			Committee: map[spectypes.OperatorID]*beacon.Node{
				spectypes.OperatorID(1): {
					IbftID: 1,
					Pk:     secretKeys[1].GetPublicKey().Serialize(),
				},
			},
			Metadata:     nil,
			OwnerAddress: "",
			Operators:    nil,
		},
		ChangeRoundStorage: changeRoundStorage,
		Identifier:         spectypes.NewMsgID([]byte("pk"), spectypes.BNRoleAttester),
		Fork:               forksfactory.NewFork(forksprotocol.GenesisForkVersion),
		ReadMode:           true,
	}

	tests := []struct {
		name                 string
		withPrepareValue     bool
		shouldFailValidation bool
		expectedHeight       specqbft.Height
		expectedRound        specqbft.Round
		signedMsg            *specqbft.SignedMessage
	}{
		{
			"first change round (height 0)",
			false,
			false,
			0,
			1,
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     0,
				Round:      1,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedRound:            1,
					RoundChangeJustification: []*specqbft.SignedMessage{},
				}),
			}),
		},
		{
			"change round with higher round (height 0)",
			false,
			false,
			0,
			2,
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     0,
				Round:      2,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedRound:            2,
					RoundChangeJustification: []*specqbft.SignedMessage{},
				}),
			}),
		},
		{
			"change round with higher height (height 1)",
			false,
			false,
			1,
			2,
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      2,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedRound:            1,
					RoundChangeJustification: []*specqbft.SignedMessage{},
				}),
			}),
		},
		{
			"change round with lower round (height 1)",
			false,
			false,
			1,
			2, // expecting the last one
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     1,
				Round:      1,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedRound:            2,
					RoundChangeJustification: []*specqbft.SignedMessage{},
				}),
			}),
		},
		{
			"change round with lower height (height 0)",
			false,
			false,
			1, // expecting the last one
			2,
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     0,
				Round:      1,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedRound:            2,
					RoundChangeJustification: []*specqbft.SignedMessage{},
				}),
			}),
		},
		{
			"change round with prepare data",
			true,
			false,
			2,
			2,
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     2,
				Round:      2,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 1,
					RoundChangeJustification: []*specqbft.SignedMessage{
						testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
							MsgType:    specqbft.PrepareMsgType,
							Height:     2,
							Round:      1,
							Identifier: ctrl.Identifier[:],
							Data:       prepareDataToByte(t, &specqbft.PrepareData{Data: []byte("value")}),
						}),
					},
				}),
			}),
		},
		{
			"change round with invalid prepare data",
			true,
			true,
			2,
			2,
			testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
				MsgType:    specqbft.RoundChangeMsgType,
				Height:     3,
				Round:      1,
				Identifier: ctrl.Identifier[:],
				Data: changeRoundDataToByte(t, &specqbft.RoundChangeData{
					PreparedValue: []byte("value"),
					PreparedRound: 1,
					RoundChangeJustification: []*specqbft.SignedMessage{
						testing2.SignMsg(t, secretKeys, []spectypes.OperatorID{spectypes.OperatorID(1)}, &specqbft.Message{
							MsgType:    specqbft.PrepareMsgType,
							Height:     2,
							Round:      1,
							Identifier: ctrl.Identifier[:],
							Data:       prepareDataToByte(t, &specqbft.PrepareData{Data: []byte("value_invalid")}),
						}),
					},
				}),
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ctrl.ProcessChangeRound(test.signedMsg)
			if test.shouldFailValidation {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			res, err := ctrl.ChangeRoundStorage.GetLastChangeRoundMsg(ctrl.Identifier, test.signedMsg.GetSigners()...)
			require.Equal(t, 1, len(res))
			last := res[0]
			require.NoError(t, err)
			require.NotNil(t, last)
			require.Equal(t, test.expectedHeight, last.Message.Height)
			require.Equal(t, test.expectedRound, last.Message.Round)
			if test.withPrepareValue {
				crd, err := last.Message.GetRoundChangeData()
				require.NoError(t, err)
				require.Equal(t, crd.PreparedValue, []byte("value"))
			}
		})
	}
}

func changeRoundDataToByte(t *testing.T, crd *specqbft.RoundChangeData) []byte {
	encoded, err := crd.Encode()
	require.NoError(t, err)
	return encoded
}

func prepareDataToByte(t *testing.T, pd *specqbft.PrepareData) []byte {
	encoded, err := pd.Encode()
	require.NoError(t, err)
	return encoded
}
