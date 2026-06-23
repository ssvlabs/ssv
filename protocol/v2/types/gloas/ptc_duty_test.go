package gloas

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestPTCDuty_JSON(t *testing.T) {
	d := &PTCDuty{
		PubKey:         phase0.BLSPubKey{0x11, 0x22},
		ValidatorIndex: 1234,
		Slot:           567,
	}
	b, err := json.Marshal(d)
	require.NoError(t, err)
	// Lock the beacon-API wire form: pubkey as 0x-hex, validator_index and slot as decimal strings.
	require.JSONEq(t, fmt.Sprintf(`{"pubkey":"%#x","validator_index":"1234","slot":"567"}`, d.PubKey), string(b))

	var dec PTCDuty
	require.NoError(t, json.Unmarshal(b, &dec))
	require.Equal(t, d, &dec)
}
