package eventhandler

import (
	"context"
	"encoding/binary"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/eth/executionclient"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
)

// const rawOperatorAdded = `{
// 	"address": "0x3A23a7F455E853058d900f5dc86f1Bb1589b54F9",
// 	"topics": [
// 	  "0xd839f31c14bd632f424e307b36abff63ca33684f77f28e35dc13718ef338f7f4",
// 	  "0x0000000000000000000000000000000000000000000000000000000000000001",
// 	  "0x0000000000000000000000009d4d2d2dd7f11953535691786690610512e26b6c"
// 	],
// 	"data": "0x0000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000deb9cd9e0000000000000000000000000000000000000000000000000000000000000002c0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000002644c5330744c5331435255644a54694253553045675546564354456c4449457446575330744c533074436b314a53554a4a616b464f516d64726357687261556335647a424351564646526b464254304e425554684254556c4a516b4e6e53304e42555556424d5667324d5546585930303151554e4c61474e354d546c556145494b627939484d576c684e3142794f565572616c4a35615759355a6a4179524739736430393156325a4c4c7a645356556c684f45684562484276516c564552446b77525456515547644a5379397354584234527974586277707751324e3562544270576b395554304a7a4e44453562456833547a41346258466a61314a735a4567355745786d626d59325554687157465235596d3179597a64574e6d77794e56707263546c3455306f7762485231436e646d546e5654537a4e435a6e46744e6b51784f55593061545643626d56615357686a52564a54596c464c5744467862574e71596e5a464c326379516b6f34547a68615a5567726430527a54484a694e6e5a585156494b5933425957473175656c4533566c70365a6b6c485447564c5655314354546836535730726358493452475a34534568536556553151544533634655346379394d4e5570355258453152474a6a6332513264486c6e625170355545394259554e7a576c6456524549335547684c4f487055575539575969394d4d316c6e53545534626a4658656b35494d307335636d467265557070546d55785445394756565a7a51544644556e68745132597a436d6c525355524255554643436930744c5330745255354549464a545153425156554a4d53554d675330565a4c5330744c53304b00000000000000000000000000000000000000000000000000000000",
// 	"blockNumber": "0x843735",
// 	"transactionHash": "0x4f4f9c1e37cf0800a201227e8fa3cad6f8f246ac1cca1cb90e2c3311538b300c"
//   }`

const rawValidatorAdded = `{
	"address": "0x4b133c68a084b8a88f72edcd7944b69c8d545f03",
	"topics": [
	  "0x48a3ea0796746043948f6341d17ff8200937b99262a0b48c2663b951ed7114e5",
	  "0x00000000000000000000000077fc6e8b24a623725d935bc88057098d0bca6eb3"
	],
	"data": "0x000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001a0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000017c4071580000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000532d024bb0158000000000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000300000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000030b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000520823f86dfcae5771e9b847c07ed6dda49211274db079f3397b3b06ab7291cebd688f63cb5648ca535c4ec4f9e2b89a48515a035dc24f4738ada942b72d32f75a7e72ac8a8c39d73a6fd633d0f58cadc4618dc70c8160cab5573d541c88a6aebada0acbbeacd49f2931c8c0c548d1b0f69cd468c803ec3fe06bddf08186ae3b64e0b5f1762feb141a06e71c828cedd3c878a08a40fd84d3a0449308c458fd324e67eb6df89e28bf6c304a1e71dcb8c9823b85c2dcca82a980cffb994da194e70b487d02db1ff328b6a8d5f6b519ffc1b524753ce8ed56d4ec1a3cdddc3b24f427b22caa351a1fa0d9f523bd279756ec38080c4850691c1f520dee49a9f4267c0a7a53c7818d165681c9a615a391ee5e4cc9c0e51725c1a92f7e92a393afe4f8f3f35503f241288822a1d721f5c164ea7f85d2b43b638678593d670e79c17a0e1398ac6bbd3ef7ccbdf67e38f6d058ffc5319280868c9a44529b1a4ea7f73792680e67693c502ad9053935edc312c48a94a0d18c71c18af8eb46ae8979d30f176969063430c14ef18a51b3dca4f8775551f4e1fc6a651ee909fc4f7b872c041514a01b4d88b972be86960e3472419ef1577c92af61d572e4e07b32bb38a0e52a1c75f03e1cee80d053b97e3e238c022521c48c6c1dc0f0def8fa0472d7669095b0e9304e63af8b5a9928d9fcf4de166029d88891d10ae6abafe150cc6e9aa6464d76064b16a19b09dad4556dffa580d14cd6755fa2274022abbc917eb7a50f296a153781742c2f101cf280b7f095bf443d51be4dcdd114804fb40ba496c16a3c3a3e82d8645833e51c22cebb78ce4b18b6b9eb3b480f5478b3ed97b5a93b705f41d05ed8423f424c5d05317c4e6e53e954570a46027361f7f3f18a581860720dac25afc00f4378a35439fd860ccbc0f0586ae6cf44d53f828faf77c1949bfe58c428de7263d48b1f7ecbb24a25c6abc11aa6105fe41a9a1f608c6895d808e8ca805efd306ec8774201a63e7d9220e031c5e8abdde49f5d56590637a5234b4b35a20875d5e0a5b06c2834dae47dd50633c371ef1071ea3d79a8ce727c2e83d3fae85a596112404875e847c743ddde50bc13b5d661f558e4d02f29b972188418d2f875d0603abbe9ab5c1fc19ae80636d9aa3a6c80be21b2970b84aa4659244424f943b3a99c8ec73304bcc8fc51519f1655ad6f75954af3cb238e946ac50aa365fd6538a7190b6e64b320f822a0010e92e1e4f3d773d25c4e29b3d590e75b4ebfecd6c059df2060f44344dda27f2f794aeb3dfcde62c7b24b80ff95ff1246d05805a12028d9840316f6b8368b60d2a824cc14b02d25a46b689e4519dd9963b5786ff9fa0c695fbdff455499a8f6cc88261b498e90223c0abca38dfe188eef0ac4680f6ac172fdf6b4a343cb1f090a8ce427ef4c745a2408f9ffe67c5b8eb17e7cc2e5851db5f5c75c0658afea00dc1552caf7ef745d2e5e057ccd3b177de22d989fe97bcac17471d0e8ee330a6d9788c6927b1991e784ec61deef91afad21895718e3fa751a782cf66c5911e3f2148dffb01e7e09dcbce8053e060df505f1121202017b34010ddbf02e63b40b8e88a73ac75eb239c401f136b255aded2201de167c9b6ee140de2d307712c8db8e958c5bab3de27d6a40e0b1211ccb634ca9204ce1bda71064f3bee1546f97979c9ec07cd5b4cb5befd7cf8ac930ad74111f381c72d18d3cf1aefef073630cc7bfef722650023032d6493fea494b3b01c95790b08609c9c039a1849fbe47042a29e98ce92f87641647db7d610357c087de95218ea357284828925c21ff7685f01f1b0e5ebadef7d1e763c64ee06d29f4ded10075d39",
	"blockNumber": "0x89EBFF",
	"transactionHash": "0x921a3f836fb873a40aa4f83097e52b69225334c49674dc262b2bb90d27e3a801"
  }`

func TestExecuteTask(t *testing.T) {
	logger, _ := setupLogsCapture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Create operators rsa keys

	ops, err := createOperators(1, 0)
	require.NoError(t, err)

	eh, validatorCtrl, err := setupEventHandler(t, ctx, logger, nil, ops[0], true)
	require.NoError(t, err)

	valPk := "b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"

	t.Run("test StopValidator task execution", func(t *testing.T) {
		validatorCtrl.EXPECT().StopValidator(gomock.Any()).Return(nil).AnyTimes()

		task := NewStopValidatorTask(eh.taskExecutor, spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)))
		require.NoError(t, task.Execute())
	})

	t.Run("test LiquidateCluster task execution", func(t *testing.T) {
		var shares []*ssvtypes.SSVShare
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)),
			},
		}

		validatorCtrl.EXPECT().LiquidateCluster(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		shares = append(shares, share)
		task := NewLiquidateClusterTask(eh.taskExecutor, ethcommon.HexToAddress("0x1"), []uint64{1, 2, 3}, shares)
		require.NoError(t, task.Execute())
	})
	t.Run("test ReactivateCluster task execution", func(t *testing.T) {
		var shares []*ssvtypes.SSVShare
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)),
			},
		}

		shares = append(shares, share)
		task := NewReactivateClusterTask(eh.taskExecutor, ethcommon.HexToAddress("0x1"), []uint64{1, 2, 3}, shares)
		validatorCtrl.EXPECT().ReactivateCluster(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		require.NoError(t, task.Execute())
	})
	t.Run("test UpdateFeeRecipient task execution", func(t *testing.T) {
		task := NewUpdateFeeRecipientTask(eh.taskExecutor, ethcommon.HexToAddress("0x1"), ethcommon.HexToAddress("0x2"))
		validatorCtrl.EXPECT().UpdateFeeRecipient(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		require.NoError(t, task.Execute())
	})
}

func TestHandleBlockEventsStreamWithExecution(t *testing.T) {
	logger, observedLogs := setupLogsCapture()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create operators rsa keys
	ops, err := createOperators(1, 0)
	require.NoError(t, err)

	eh, _, err := setupEventHandler(t, ctx, logger, nil, ops[0], false)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []spectypes.OperatorID{1, 2, 3, 4} {
		od := &storage.OperatorData{
			PublicKey:    binary.LittleEndian.AppendUint64(nil, id),
			OwnerAddress: ethcommon.Address{},
			ID:           id,
		}

		found, err := eh.nodeStorage.SaveOperatorData(nil, od)
		require.NoError(t, err)
		require.False(t, found)
	}

	eventsCh := make(chan executionclient.BlockLogs)
	go func() {
		defer close(eventsCh)

		logValidatorAdded := unmarshalLog(t, rawValidatorAdded)
		events := []ethtypes.Log{
			logValidatorAdded,
		}

		for _, blockLogs := range executionclient.PackLogs(events) {
			eventsCh <- blockLogs
		}
	}()

	lastProcessedBlock, err := eh.HandleBlockEventsStream(ctx, eventsCh, true)
	require.Equal(t, uint64(0x89EBFF), lastProcessedBlock)
	require.NoError(t, err)

	var observedLogsFlow []string
	for _, entry := range observedLogs.All() {
		observedLogsFlow = append(observedLogsFlow, entry.Message)
	}
	happyFlow := []string{
		"setting up validator controller",
		"malformed event: failed to verify signature",
		"processed events from block",
	}
	require.Equal(t, happyFlow, observedLogsFlow)
}

func setupLogsCapture() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	return zap.New(core), logs
}
