package eth1

import (
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestLocalEventsUnmarshalYAML(t *testing.T) {
	t.Run("Fail to unmarshal event without event name", func(t *testing.T) {
		input := []byte(`
- Log:
  Name:
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "event name is empty")
	})

	t.Run("Fail to unmarshal unknown event", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: UnknownEvent
  Data:
    Id: 1
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "event unknown")
	})

	t.Run("Fail to unmarshal event with empty data", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: OperatorAdded
  Data:
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "event data is nil")
	})
}

func TestUnmarshalYAMLOperatorAddedEvent(t *testing.T) {
	t.Run("Successfully unmarshal OperatorAdded event", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: OperatorAdded
  Data:
    Id: 1
    Owner: 0x97a6C1f3aaB5427B901fb135ED492749191C0f1F
    PublicKey: LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBdVRFMVpuZGtubjdqOHR0VmNwd1cKRWFJNnJaZHh1VzM4L01URmdCRTN2Q3g0TTVMNzdRb3dhZVEwQ0lqTkhEdzNDZlhoM3pQRVp1c05ER1cwcGVEbwp6QkN1Ykk0UlBQd1JaaThaejdRS0ZxdFNUNUZYa3FjVEdYVmNPb2dla3dXRG5LMVU2OTkxc2VJZ01tVTBxbTc4CklpSW8zZDQrVG9Dd3J5MDdKNkprNVZGY1N2MHVmVlNvN0FicE5HWFp2aldqN2NWSWZIZENONGljcHhFaUhuWEsKNVlWem8zVXBaRGRVZUlSS1daeUVLczdSejdUKytFNWY0eWp4eThmTG56VlVSMFd4Yys4UjBNMm5GRUczZ1NJTApSaTRoVTFRK2x6K1d1cEFwcFVMU2MwUFJOVFBQQkRTQWM5RXlVQjAzSmkzMnhwdmJDc05hNHhDZzNrZjgyZk1pCjV3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K
`)
		var parsedData []*Event
		require.NoError(t, yaml.Unmarshal(input, &parsedData))
		require.NotNil(t, parsedData)
		require.Equal(t, 1, len(parsedData))
		require.Equal(t, "OperatorAdded", parsedData[0].Name)
		require.NotNil(t, parsedData[0].Data)
		eventData, ok := parsedData[0].Data.(abiparser.OperatorAddedEvent)
		require.True(t, ok)
		require.Equal(t, uint64(1), eventData.Id)
		require.Equal(t, "0x97a6C1f3aaB5427B901fb135ED492749191C0f1F", eventData.Owner.String())
		require.Equal(t, []byte("LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBdVRFMVpuZGtubjdqOHR0VmNwd1cKRWFJNnJaZHh1VzM4L01URmdCRTN2Q3g0TTVMNzdRb3dhZVEwQ0lqTkhEdzNDZlhoM3pQRVp1c05ER1cwcGVEbwp6QkN1Ykk0UlBQd1JaaThaejdRS0ZxdFNUNUZYa3FjVEdYVmNPb2dla3dXRG5LMVU2OTkxc2VJZ01tVTBxbTc4CklpSW8zZDQrVG9Dd3J5MDdKNkprNVZGY1N2MHVmVlNvN0FicE5HWFp2aldqN2NWSWZIZENONGljcHhFaUhuWEsKNVlWem8zVXBaRGRVZUlSS1daeUVLczdSejdUKytFNWY0eWp4eThmTG56VlVSMFd4Yys4UjBNMm5GRUczZ1NJTApSaTRoVTFRK2x6K1d1cEFwcFVMU2MwUFJOVFBQQkRTQWM5RXlVQjAzSmkzMnhwdmJDc05hNHhDZzNrZjgyZk1pCjV3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K"), eventData.PublicKey)
	})

	t.Run("Fail to unmarshal OperatorAdded event with non number operator Id", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: OperatorAdded
  Data:
    Id: id
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "yaml: unmarshal errors:\n  line 5: cannot unmarshal !!str `id` into uint64")
	})
}

func TestUnmarshalYAMLValidatorAddedEvent(t *testing.T) {
	t.Run("Successfully unmarshal ValidatorAdded event", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: ValidatorAdded
  Data:
    PublicKey: 0x89913833b5533c1089a957ea185daecc0c5719165f7cbb7ba971c2dcf916be32d6c620dab56888bc278515bf27aebc5f
    OwnerAddress: 0xcEEfd323DD28a8d9514EDDfeC45a6c81800A7D49
    OperatorIds: [1, 2, 3, 4]
    SharePublicKeys:
      - 0x8f6c6caf14da98dc5b45482613d709be3620c5094ddc6958002263058cc2fe4bdb7d9801d205d0eca13601b2c5491854
      - 0x92453e6b59c0c443c87289ef6aa0c00b6e6c8da68a78004c7493fc3a0d7b5360ed493c273c6e24ecba4e059e198d9ea7
      - 0xb54f165b8da223b4e16d85e44f0d84d6dda9b0a809d609c2ae9bf31cb4814f7f8f56ed91d6e19e5369bec6f2ebce234f
      - 0x9880ace7699a9983ac4c9157ac7094ff996ceb8dc546f482654047eef57ca35684bdb0992fa939f13e60f9ccd2f20f01
    EncryptedKeys:
      - BsiD3YeOc46Aw/o0aZMc9KqmxkUiemEy+b5x2XcnLY6YAfTKjKkbp14h0KCudSNoPrr3mQRRWzB9T43U9RvApy0XBEWSGeyOeAH+Rn44HEKIC5XHqVyZn2EIbvgdp4+H3VqgGu5lA+Amkg6UVFdtm1+lzdM9961J+aH9xUOtYNknnWYezijIVt4cihdTI2gG9CgNM5csPAQHt278jQoln2hL1NPTBMBu8tQlDz5duHxHAg3wtIwIE3SifKTHC4NDCCZ2rRxne0feVnciPy+BTAQMm/VY/8CLX48nX9rKtaP3e13VdIZ2CIHetJk0zHjxqXAG6h0me+43Y+vW5B1z9A==
      - TUpAW/VM/Tiv9IjeCjQK3lrz7zgTR5a1hYlRGX+2jUcI8oBKcsvGoY7KfViGLtalWGIiqsUUlVbr5oqpVe307ygnqY6LtRMkNPKs5rAkPULZ3Rt3Fc5wYiffJLZC5KhrJwLyQSqRd83Ll5BbFCJS4Y6ZVc6lItuoazqbTaZiGX6uIkISFCq0nqw9XrDe3ZXeHPBbH9bbVuoG93nlv5zhcJn1hrRzRukbiH91ZB6FyLmFlK4r6OAdQMIm7Cb68QFtBBiwBuuK4SleJ2ML2IAq420sk+a1SsVguU5bIz62SPDRBX26oIUrRV37rmFAO6bhdy1mVM/1ZrhoZOJ8Lcf6zg==
      - XhfhHdJF62VlgVMsLv/VJVqBpgCdaBlDi5FNPxEhe5G8Y1PrhZjJW8G/BoLV80/l+H6Xaba2cMeEyxnKDBsTCGZoQ1SQMsqrHQSVr3qcLDLg+ZcvEdxB1bjNgsE7CFWHrJ7j8gvMbpggywXKcFh6HUJZAv7lna6vaWJg8AYV1xdy5k/aLBWlynSDeqXlSh6izQ4qpWzhmaRWl4S3i3vHvLPepUhXER4K4DaVRmANgd64mBlXQ6iNAiczkALQxUzxVBXlDfkXAr9jA1zV7Yuy51o9fL8dSvg+QDs7Ng8KCJOtfsf2uS2v7GoxFDTMi7ZhN4RJnYnYIxrvFYJfNAd7LA==
      - LqWGlGleqouKBRvt3iJ43hMu/T8YIOpEeTgPuYSFEad1SH42PZQzGRUD/DKHfenW0i8ARXVWzZCdWRcfF8+2wph+J4MW7EvGnJpVd/ETcdrq9Ep0M0ypU+GxlfqeB/LdXskLn7XGSuAkWDyg2oLjwSlGVbWSB5Yd4zuU/VSwkfTDckmxIjdmvGLgBbhBic2PragYoHYKs3U6uz9PUUapavhlRBaSgqayTEm5/u8uT+X6IraaDAGzzh7i3lztLulj8zq1WWXArxfWXmPlCZt2N654elJSzmwF14+22Y691NgEc2NazRp9lukU8PfwolrRm7w9wzTtpl3UOnwuPtBOxw==
`)
		var parsedData []*Event
		require.NoError(t, yaml.Unmarshal(input, &parsedData))
	})

	t.Run("Fail to unmarshal ValidatorAdded event with non numbers operator-ids ", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: ValidatorAdded
  Data:
    PublicKey: 0x89913833b5533c1089a957ea185daecc0c5719165f7cbb7ba971c2dcf916be32d6c620dab56888bc278515bf27aebc5f
    OwnerAddress: 0xcEEfd323DD28a8d9514EDDfeC45a6c81800A7D49
    OperatorIds: [one, two, three, four]
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "yaml: unmarshal errors:\n  line 7: cannot unmarshal !!str `one` into uint64\n  line 7: cannot unmarshal !!str `two` into uint64\n  line 7: cannot unmarshal !!str `three` into uint64\n  line 7: cannot unmarshal !!str `four` into uint64")
	})

	t.Run("Fail to unmarshal ValidatorAdded event with non array operator-ids", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: ValidatorAdded
  Data:
    PublicKey: 0x89913833b5533c1089a957ea185daecc0c5719165f7cbb7ba971c2dcf916be32d6c620dab56888bc278515bf27aebc5f
    OwnerAddress: 0xcEEfd323DD28a8d9514EDDfeC45a6c81800A7D49
    OperatorIds: 1, 2, 3, 4
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "yaml: unmarshal errors:\n  line 7: cannot unmarshal !!str `1, 2, 3, 4` into []uint64")
	})

	t.Run("Fail to unmarshal ValidatorAdded event with non hex encoded share public keys", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: ValidatorAdded
  Data:
    PublicKey: 0x89913833b5533c1089a957ea185daecc0c5719165f7cbb7ba971c2dcf916be32d6c620dab56888bc278515bf27aebc5f
    OwnerAddress: 0xcEEfd323DD28a8d9514EDDfeC45a6c81800A7D49
    OperatorIds: [1, 2, 3, 4]
    SharePublicKeys:
      - sss
`)
		var parsedData []*Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "encoding/hex: invalid byte: U+0073 's'")
	})
}
