package valcheck

import (
	"encoding/json"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type testSigner struct {
	returnSlashable bool
}

func (s *testSigner) AddShare(shareKey *bls.SecretKey) error {
	return nil
}

func (s *testSigner) SignIBFTMessage(message *proto.Message, pk []byte) ([]byte, error) {
	return nil, nil
}

func (s *testSigner) SignAttestation(data *spec.AttestationData, duty *beacon.Duty, pk []byte) (*spec.Attestation, []byte, error) {
	return nil, nil, nil
}

func (s *testSigner) IsAttestationSlashable(data *spec.AttestationData, pk []byte) error {
	if s.returnSlashable {
		return errors.New("slashable")
	}
	return nil
}

func TestAttestationValueCheck_Check(t *testing.T) {
	t.Run("valid and not slashable", func(t *testing.T) {
		attestationData := &spec.AttestationData{
			Slot:            30,
			Index:           1,
			BeaconBlockRoot: [32]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2},
			Source: &spec.Checkpoint{
				Epoch: 1,
				Root:  [32]byte{},
			},
			Target: &spec.Checkpoint{
				Epoch: 3,
				Root:  [32]byte{},
			},
		}
		byts, err := attestationData.MarshalSSZ()
		require.NoError(t, err)

		check := AttestationValueCheck{
			signer: &testSigner{
				returnSlashable: false,
			},
		}

		require.NoError(t, check.Check(byts, []byte{}))
	})

	t.Run("invalid decoding", func(t *testing.T) {
		attestationData := &spec.AttestationData{
			Slot:            30,
			Index:           1,
			BeaconBlockRoot: [32]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2},
			Source: &spec.Checkpoint{
				Epoch: 1,
				Root:  [32]byte{},
			},
			Target: &spec.Checkpoint{
				Epoch: 3,
				Root:  [32]byte{},
			},
		}
		byts, err := json.Marshal(attestationData)
		require.NoError(t, err)

		check := AttestationValueCheck{
			signer: &testSigner{
				returnSlashable: false,
			},
		}

		require.EqualError(t, check.Check(byts, []byte{}), "could not parse input value storing attestation data: incorrect size")
	})

	t.Run("slashable", func(t *testing.T) {
		attestationData := &spec.AttestationData{
			Slot:            30,
			Index:           1,
			BeaconBlockRoot: [32]byte{1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2},
			Source: &spec.Checkpoint{
				Epoch: 1,
				Root:  [32]byte{},
			},
			Target: &spec.Checkpoint{
				Epoch: 3,
				Root:  [32]byte{},
			},
		}
		byts, err := attestationData.MarshalSSZ()
		require.NoError(t, err)

		check := AttestationValueCheck{
			signer: &testSigner{
				returnSlashable: true,
			},
		}

		require.EqualError(t, check.Check(byts, []byte{}), "slashable")
	})
}
