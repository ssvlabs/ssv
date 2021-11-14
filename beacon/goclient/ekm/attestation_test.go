package ekm

import (
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/beacon"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSignAttestation(t *testing.T) {
	km := testKeyManager(t)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))
	require.NoError(t, km.AddShare(sk1))

	duty := &beacon.Duty{
		Type:                    beacon.RoleTypeAttester,
		PubKey:                  [48]byte{},
		Slot:                    30,
		ValidatorIndex:          1,
		CommitteeIndex:          2,
		CommitteeLength:         128,
		CommitteesAtSlot:        4,
		ValidatorCommitteeIndex: 3,
	}
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

	t.Run("sign once", func(t *testing.T) {
		_, sig, err := km.SignAttestation(attestationData, duty, sk1.GetPublicKey().Serialize())
		require.NoError(t, err)
		require.NotNil(t, sig)
	})
	t.Run("slashable sign, fail", func(t *testing.T) {
		attestationData.BeaconBlockRoot = [32]byte{2, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2, 3, 4, 5, 6, 1, 2}
		_, sig, err := km.SignAttestation(attestationData, duty, sk1.GetPublicKey().Serialize())
		require.EqualError(t, err, "failed to sign attestation: slashable attestation (HighestAttestationVote), not signing")
		require.Nil(t, sig)
	})
}
