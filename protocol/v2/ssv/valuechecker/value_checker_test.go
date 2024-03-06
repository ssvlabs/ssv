package valuechecker

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/threshold"
)

func TestValueChecker(t *testing.T) {
	threshold.Init()

	logger := logging.TestLogger(t)

	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	currentSlot := &utils.SlotValue{}
	currentSlot.SetSlot(32)

	networkConfig := networkconfig.NetworkConfig{
		Beacon: utils.SetupMockBeaconNetwork(t, currentSlot),
		Domain: networkconfig.TestNetwork.Domain,
	}

	shareSK := &bls.SecretKey{}
	shareSK.SetByCSPRNG()
	sharePKBytes := shareSK.GetPublicKey().Serialize()

	km, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, true, "")
	require.NoError(t, err)

	require.NoError(t, km.AddShare(shareSK))

	attestationData := &phase0.AttestationData{
		Slot:            currentSlot.GetSlot(),
		Index:           3,
		BeaconBlockRoot: phase0.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2},
		Source: &phase0.Checkpoint{
			Epoch: 1,
			Root:  phase0.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2},
		},
		Target: &phase0.Checkpoint{
			Epoch: 2,
			Root:  phase0.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2},
		},
	}

	attestationDataBytes, err := attestationData.MarshalSSZ()
	require.NoError(t, err)

	consensusData := &spectypes.ConsensusData{
		Duty: spectypes.Duty{
			Type:                    spectypes.BNRoleAttester,
			PubKey:                  testingutils.TestingValidatorPubKey,
			Slot:                    currentSlot.GetSlot(),
			ValidatorIndex:          testingutils.TestingValidatorIndex,
			CommitteeIndex:          3,
			CommitteesAtSlot:        36,
			CommitteeLength:         128,
			ValidatorCommitteeIndex: 11,
		},
		DataSSZ: attestationDataBytes,
		Version: spec.DataVersionPhase0,
	}

	vc := New(km, networkConfig.Beacon, consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes)

	t.Run("same attestation not slashable", func(t *testing.T) {
		_, _, err = km.SignBeaconObject(attestationData, phase0.Domain{}, sharePKBytes, spectypes.DomainAttester)
		require.NoError(t, err)

		data, err := consensusData.Encode()
		require.NoError(t, err)

		require.NoError(t, vc.AttesterValueCheckF(data))
	})

	t.Run("different attestation slashable", func(t *testing.T) {
		differentAttestation := &phase0.AttestationData{
			Slot:            currentSlot.GetSlot(),
			Index:           3,
			BeaconBlockRoot: phase0.Root{9, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2},
			Source: &phase0.Checkpoint{
				Epoch: 1,
				Root:  phase0.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2},
			},
			Target: &phase0.Checkpoint{
				Epoch: 2,
				Root:  phase0.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2},
			},
		}

		differentAttestationDataBytes, err := differentAttestation.MarshalSSZ()
		require.NoError(t, err)

		differentConsensusData := &spectypes.ConsensusData{
			Duty: spectypes.Duty{
				Type:                    spectypes.BNRoleAttester,
				PubKey:                  testingutils.TestingValidatorPubKey,
				Slot:                    currentSlot.GetSlot(),
				ValidatorIndex:          testingutils.TestingValidatorIndex,
				CommitteeIndex:          3,
				CommitteesAtSlot:        36,
				CommitteeLength:         128,
				ValidatorCommitteeIndex: 11,
			},
			DataSSZ: differentAttestationDataBytes,
			Version: spec.DataVersionPhase0,
		}

		data, err := differentConsensusData.Encode()
		require.NoError(t, err)

		require.NoError(t, vc.AttesterValueCheckF(data))
	})
}
