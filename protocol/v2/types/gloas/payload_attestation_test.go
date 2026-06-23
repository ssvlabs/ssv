package gloas

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func TestPayloadAttestationData_SSZ(t *testing.T) {
	d := &PayloadAttestationData{
		BeaconBlockRoot:   phase0.Root{0x01, 0x02},
		Slot:              42,
		PayloadPresent:    true,
		BlobDataAvailable: false,
	}
	require.Equal(t, 42, d.SizeSSZ())

	b, err := d.MarshalSSZ()
	require.NoError(t, err)
	require.Len(t, b, 42)

	var dec PayloadAttestationData
	require.NoError(t, dec.UnmarshalSSZ(b))
	require.Equal(t, d, &dec)

	htr1, err := d.HashTreeRoot()
	require.NoError(t, err)
	htr2, err := dec.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, htr1, htr2)
}

func TestPayloadAttestationMessage_SSZ(t *testing.T) {
	m := &PayloadAttestationMessage{
		ValidatorIndex: 7,
		Data: &PayloadAttestationData{
			BeaconBlockRoot:   phase0.Root{0xaa},
			Slot:              9,
			PayloadPresent:    true,
			BlobDataAvailable: true,
		},
		Signature: phase0.BLSSignature{0xbb, 0xcc},
	}
	require.Equal(t, 146, m.SizeSSZ())

	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	require.Len(t, b, 146)

	var dec PayloadAttestationMessage
	require.NoError(t, dec.UnmarshalSSZ(b))
	require.Equal(t, m, &dec)

	_, err = m.HashTreeRoot()
	require.NoError(t, err)
}

func TestPayloadAttestationData_JSON(t *testing.T) {
	d := &PayloadAttestationData{
		BeaconBlockRoot:   phase0.Root{0x01, 0x02},
		Slot:              42,
		PayloadPresent:    true,
		BlobDataAvailable: false,
	}
	b, err := json.Marshal(d)
	require.NoError(t, err)
	// Lock the beacon-API wire form: snake_case keys, slot as a decimal string, root as 0x-hex.
	require.JSONEq(t, `{"beacon_block_root":"0x0102000000000000000000000000000000000000000000000000000000000000","slot":"42","payload_present":true,"blob_data_available":false}`, string(b))

	var dec PayloadAttestationData
	require.NoError(t, json.Unmarshal(b, &dec))
	require.Equal(t, d, &dec)
}

func TestPayloadAttestationMessage_JSON(t *testing.T) {
	m := &PayloadAttestationMessage{
		ValidatorIndex: 7,
		Data: &PayloadAttestationData{
			BeaconBlockRoot:   phase0.Root{0xaa},
			Slot:              9,
			PayloadPresent:    true,
			BlobDataAvailable: true,
		},
		Signature: phase0.BLSSignature{0xbb, 0xcc},
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	dataJSON, err := json.Marshal(m.Data)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"validator_index":"7","data":%s,"signature":"%#x"}`, dataJSON, m.Signature), string(b))

	var dec PayloadAttestationMessage
	require.NoError(t, json.Unmarshal(b, &dec))
	require.Equal(t, m, &dec)
}
