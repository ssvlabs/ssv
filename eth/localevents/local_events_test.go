package localevents_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/localevents"
)

func TestLocalEventsUnmarshalYAML(t *testing.T) {
	t.Run("Fail to unmarshal event without event name", func(t *testing.T) {
		input := []byte(`
- Log:
  Name:
`)
		var parsedData []*localevents.Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "event name is empty")
	})

	t.Run("Fail to unmarshal unknown event", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: UnknownEvent
  Data:
    ID: 1
`)
		var parsedData []*localevents.Event
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
		var parsedData []*localevents.Event
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
    ID: 1
    Owner: 0x97a6C1f3aaB5427B901fb135ED492749191C0f1F
    PublicKey: LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBdVRFMVpuZGtubjdqOHR0VmNwd1cKRWFJNnJaZHh1VzM4L01URmdCRTN2Q3g0TTVMNzdRb3dhZVEwQ0lqTkhEdzNDZlhoM3pQRVp1c05ER1cwcGVEbwp6QkN1Ykk0UlBQd1JaaThaejdRS0ZxdFNUNUZYa3FjVEdYVmNPb2dla3dXRG5LMVU2OTkxc2VJZ01tVTBxbTc4CklpSW8zZDQrVG9Dd3J5MDdKNkprNVZGY1N2MHVmVlNvN0FicE5HWFp2aldqN2NWSWZIZENONGljcHhFaUhuWEsKNVlWem8zVXBaRGRVZUlSS1daeUVLczdSejdUKytFNWY0eWp4eThmTG56VlVSMFd4Yys4UjBNMm5GRUczZ1NJTApSaTRoVTFRK2x6K1d1cEFwcFVMU2MwUFJOVFBQQkRTQWM5RXlVQjAzSmkzMnhwdmJDc05hNHhDZzNrZjgyZk1pCjV3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K
`)
		var parsedData []*localevents.Event
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
	})

	t.Run("Fail to unmarshal OperatorAdded event with non number operator ID", func(t *testing.T) {
		input := []byte(`
- Log:
  Name: OperatorAdded
  Data:
    ID: id
`)
		var parsedData []*localevents.Event
		err := yaml.Unmarshal(input, &parsedData)
		require.Error(t, err)
		require.EqualError(t, err, "yaml: unmarshal errors:\n  line 5: cannot unmarshal !!str `id` into uint64")
	})
}
