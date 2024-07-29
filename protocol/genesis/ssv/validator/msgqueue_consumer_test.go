package validator

//
//import (
//	protocoltesting "github.com/ssvlabs/ssv/protocol/genesis/testing"
//	"testing"
//
//	"github.com/ssvlabs/ssv-spec-pre-cc/qbft"
//	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
//	"github.com/ssvlabs/ssv-spec-pre-cc/types/testingutils"
//	"github.com/stretchr/testify/require"
//)
//
//func TestMessageConsumer(t *testing.T) {
//	validator := testingutils.BaseValidator(testingutils.Testing4SharesSet())
//	validator.DutyRunners[genesisspectypes.BNRoleAttester] = testingutils.AttesterRunner(testingutils.Testing4SharesSet())
//
//	mid := genesisspectypes.NewMsgID(testingutils.TestingValidatorPubKey[:], genesisspectypes.BNRoleAttester)
//	consensusDataBytes, err := testingutils.TestAttesterConsensusData.Encode()
//	require.NoError(t, err)
//	commitData := qbft.CommitData{Data: consensusDataBytes}
//	commitDataBytes, err := commitData.Encode()
//	signedMsg := protocoltesting.SignMsg(t, testingutils.Testing4SharesSet().Shares, []genesisspectypes.OperatorID{1, 2, 3, 4}, &qbft.Message{
//		MsgType:    qbft.CommitMsgType,
//		Height:     100,
//		Round:      2,
//		Identifier: mid[:],
//		Data:       commitDataBytes,
//	})
//	msgData, err := signedMsg.Encode()
//	require.NoError(t, err)
//
//	err = validator.ProcessMessage(&genesisspectypes.SSVMessage{
//		MsgType: genesisspectypes.SSVConsensusMsgType,
//		MsgID:   mid,
//		Data:    msgData,
//	})
//	require.NoError(t, err)
//}
