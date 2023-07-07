package eventdatahandler

import (
	"testing"
	"github.com/bloxapp/ssv/eth/localevents"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)


func TestUnmarshalAndHandleYAMLOperatorAddedEvent(t *testing.T) {
	// events := []ethtypes.Log{}
	// logger := zaptest.NewLogger(t)
	// eb := eventbatcher.NewEventBatcher()

	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()
	// edh, err := setupDataHandler(t, ctx, logger)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	input := []byte(`
- Log:
  Name: OperatorAdded
  Data:
    ID: 1
    Owner: 0x97a6C1f3aaB5427B901fb135ED492749191C0f1F
    PublicKey: LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBdVRFMVpuZGtubjdqOHR0VmNwd1cKRWFJNnJaZHh1VzM4L01URmdCRTN2Q3g0TTVMNzdRb3dhZVEwQ0lqTkhEdzNDZlhoM3pQRVp1c05ER1cwcGVEbwp6QkN1Ykk0UlBQd1JaaThaejdRS0ZxdFNUNUZYa3FjVEdYVmNPb2dla3dXRG5LMVU2OTkxc2VJZ01tVTBxbTc4CklpSW8zZDQrVG9Dd3J5MDdKNkprNVZGY1N2MHVmVlNvN0FicE5HWFp2aldqN2NWSWZIZENONGljcHhFaUhuWEsKNVlWem8zVXBaRGRVZUlSS1daeUVLczdSejdUKytFNWY0eWp4eThmTG56VlVSMFd4Yys4UjBNMm5GRUczZ1NJTApSaTRoVTFRK2x6K1d1cEFwcFVMU2MwUFJOVFBQQkRTQWM5RXlVQjAzSmkzMnhwdmJDc05hNHhDZzNrZjgyZk1pCjV3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K
`)
	var parsedData []*localevents.LocalEvent
	err := yaml.Unmarshal(input, &parsedData)
	require.NoError(t, err)
}

