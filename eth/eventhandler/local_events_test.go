package eventhandler

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/localevents"
	"github.com/ssvlabs/ssv/registry/storage"
)

func TestHandleLocalEvent(t *testing.T) {
	// Create operators rsa keys
	ops, err := createOperators(1, 0)
	require.NoError(t, err)

	t.Run("correct OperatorAdded event", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: OperatorAdded
  Data:
    ID: 1
    Owner: 0x97a6C1f3aaB5427B901fb135ED492749191C0f1F
    PublicKey: LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBdVRFMVpuZGtubjdqOHR0VmNwd1cKRWFJNnJaZHh1VzM4L01URmdCRTN2Q3g0TTVMNzdRb3dhZVEwQ0lqTkhEdzNDZlhoM3pQRVp1c05ER1cwcGVEbwp6QkN1Ykk0UlBQd1JaaThaejdRS0ZxdFNUNUZYa3FjVEdYVmNPb2dla3dXRG5LMVU2OTkxc2VJZ01tVTBxbTc4CklpSW8zZDQrVG9Dd3J5MDdKNkprNVZGY1N2MHVmVlNvN0FicE5HWFp2aldqN2NWSWZIZENONGljcHhFaUhuWEsKNVlWem8zVXBaRGRVZUlSS1daeUVLczdSejdUKytFNWY0eWp4eThmTG56VlVSMFd4Yys4UjBNMm5GRUczZ1NJTApSaTRoVTFRK2x6K1d1cEFwcFVMU2MwUFJOVFBQQkRTQWM5RXlVQjAzSmkzMnhwdmJDc05hNHhDZzNrZjgyZk1pCjV3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K
`)
		var parsedData []localevents.Event
		require.NoError(t, yaml.Unmarshal(input, &parsedData))
		require.NotNil(t, parsedData)
		require.Equal(t, 1, len(parsedData))
		require.Equal(t, "OperatorAdded", parsedData[0].Name)
		require.NotNil(t, parsedData[0].Data)
		eventData, ok := parsedData[0].Data.(contract.ContractOperatorAdded)
		require.True(t, ok)
		require.Equal(t, uint64(1), eventData.OperatorId)
		require.Equal(t, "0x97a6C1f3aaB5427B901fb135ED492749191C0f1F", eventData.Owner.String())
		require.Equal(t, []byte("LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBdVRFMVpuZGtubjdqOHR0VmNwd1cKRWFJNnJaZHh1VzM4L01URmdCRTN2Q3g0TTVMNzdRb3dhZVEwQ0lqTkhEdzNDZlhoM3pQRVp1c05ER1cwcGVEbwp6QkN1Ykk0UlBQd1JaaThaejdRS0ZxdFNUNUZYa3FjVEdYVmNPb2dla3dXRG5LMVU2OTkxc2VJZ01tVTBxbTc4CklpSW8zZDQrVG9Dd3J5MDdKNkprNVZGY1N2MHVmVlNvN0FicE5HWFp2aldqN2NWSWZIZENONGljcHhFaUhuWEsKNVlWem8zVXBaRGRVZUlSS1daeUVLczdSejdUKytFNWY0eWp4eThmTG56VlVSMFd4Yys4UjBNMm5GRUczZ1NJTApSaTRoVTFRK2x6K1d1cEFwcFVMU2MwUFJOVFBQQkRTQWM5RXlVQjAzSmkzMnhwdmJDc05hNHhDZzNrZjgyZk1pCjV3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K"), eventData.PublicKey)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := zaptest.NewLogger(t)
		eh, _, err := setupEventHandler(t, ctx, logger, nil, ops[0], false)
		if err != nil {
			t.Fatal(err)
		}

		require.NoError(t, eh.HandleLocalEvents(ctx, parsedData))
	})

	// TODO: test correct signature
	t.Run("ValidatorAdded event with incorrect signature", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: ValidatorAdded
  Data:
    PublicKey: 0x89913833b5533c1089a957ea185daecc0c5719165f7cbb7ba971c2dcf916be32d6c620dab56888bc278515bf27aebc5f
    Owner: 0xcEEfd323DD28a8d9514EDDfeC45a6c81800A7D49
    OperatorIds: [1, 2, 3, 4]
    Shares: 0x8d9205986a9f0505daadeb124a4e89814e9174e471da0c8e5a3684c1b2bc7bfb36aca0abe4bf3c619268ce9bdac839d113ae2053f3ce8acbfd5793558efd44709282d423e9b03a1d37a70d27cc4f2a10677448879e6f45a5f8b259e08718b902b52de097cf98c9f6a5355a2060f827e320b1decfbbd974000277de3c65fdeebc4da212ec1def8ef298e7e1459a6ea912b04c8f48f50b65bf2475255cbf4c39b9d1d4dcc34b2959d7b07ed7c493edd22f290483f65f56038c1602d6210a0ad78cb5764bffb0972bc98de742d5f27cd714bbecaae9e398c5322a9b7ca7711c3ce5e952246090e05787f2f91aa2693e118b82bc1b261018b0eb9dcd3c737ccbffb55d726d72d3560d38525b83f882879065badd7cb9504b2e95a205b81c11334e7bab1a5a75539c0cca11e5cf44f51bd989a3ff5c17a68fab85a1f2ea702b667dd3e4526f476c97ad9d2826f291352ebbb8bf1cbd53d47b49ae3f16d75a6eff52184b06caf6662d61c04aca80c9f05e9cdaca8544292df8617a327d519057ace77fe72ba975d6d953d217208d644e5f9a8527f575296b712872c1932492d6bc5519eee9891b22cead112b35c7316070a6ab4c9225559023a3e57d19d7fcd9d9f1641c87d9693389ad50cc335f57f587c3ba4a18760eaea5026d217871192d58a156279b5c764476abe19af43d714474a3bc70af3fc16b0334ec0e411207290b80bd5d61b007e025cd7c640d4637a174e073bf8c357645450f85f614bf42b81dda5f1ebd165717d1c1f4da684f9520dae3702044d0fe84abda960aa1b58127a2aecbe819a9f70d9adbace7c555ec85b4e78b15d50c0b9564d9e0b6abb5e2ed249a8dca3c35fc23c1c71ae045316e95fe19cd24b960f4e1b4f498776dcd244823b5c15ca5000a7990519114dddba550fd457b2438b70ac2d8d62128536a3c5d086a1314a6129157eec322249c82084f2fed4cc6d702e06553c4288dd500463068949401543240bb2d90a57384c0c8a39e004c7ea2ca0f475dc0a0daa330d891198120ff6577c9938e2197c6fecb3974793965fda888bbe94ce6acb1a981e25ef4026d518794cad49e3bd96f7295b526389581b5f5f25de97475eb8c636bdcd4049bbd7bbc4bd8e021d8de33304d586ffb7f5d87f8000372de396d20db43458c91f7ef3e5da35177b1e438426c54235838fa3400fc85f5f5f9e49062ab082db0965e70ed163fa74ce045265c60ec2d86845028dec08e06359b0cd1ccfdf48b0b901cc8d23eecfb5cb6f558277200fffc560282260998a3d510f0e41ced979f5c7e402e41dce60628a23fa7ba68fe2a42921357de525b44d6932e57e65e084e4305bf4524fd06aa825a85ed9ad7db737db963db5deac3a5456a41c75e2848651fbcb04adb0e9c675cf9954f5a56fe3d4cc665b39b63b0011799c7a9a3e01f9a37d4885c05e49f2d5f0fa7559192b0287ad7882c09a08254e56bc4a2f9dc37d6640159596ff468697cd9fe41c1851a4adb92dd4ae8e23e2963adfed4a3a91c916f67e726f1f735a4b5b4309b923817edb668b94bf8462c6c99247e567c4a4ae0475c1c5c116b2622fd961eaf64ea4c9a6aa80f847e7c4a04eb5588d1c969be7bb23ed668e046e7ac14e72427b4cf27f37b44b4037d8cef02321d7b9beb1d7e7c1c08c4083d42c3bf579a97028d85b09aebb3f88aba882fedf8bfdbbb12c1d7221881d8688b95806800bbf1a32b08a81e629ff624fc865558dc6303bae0c74c04aa513b01248d4f22d732f60f7148080a7ccb5388f27fd902ef581b3131932e3997cfa551d61913f3c7a3ce1901ba77a2e61fd5025c4f5762e7d54048d4a9995f0ad6a6e5
`)

		var parsedData []localevents.Event
		require.NoError(t, yaml.Unmarshal(input, &parsedData))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := zaptest.NewLogger(t)
		eh, _, err := setupEventHandler(t, ctx, logger, nil, ops[0], false)
		if err != nil {
			t.Fatal(err)
		}

		for _, id := range []spectypes.OperatorID{1, 2, 3, 4} {
			od := &storage.OperatorData{
				PublicKey:    binary.LittleEndian.AppendUint64(nil, id),
				OwnerAddress: common.Address{},
				ID:           id,
			}

			found, err := eh.nodeStorage.SaveOperatorData(nil, od)
			require.NoError(t, err)
			require.False(t, found)
		}

		require.ErrorIs(t, eh.HandleLocalEvents(ctx, parsedData), ErrSignatureVerification)
	})
}
