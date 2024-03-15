package sameduty

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/ssv"
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

func TestSameDuty(t *testing.T) {
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

	km, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, false, "")
	require.NoError(t, err)

	require.NoError(t, km.AddShare(shareSK))

	t.Run("attester", func(t *testing.T) {
		vc := New(km, sharePKBytes)

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

		_, _, err = km.SignBeaconObject(attestationData, phase0.Domain{}, sharePKBytes, spectypes.DomainAttester)
		require.NoError(t, err)

		data, err := consensusData.Encode()
		require.NoError(t, err)

		require.NoError(t, vc.AttesterValueCheck(ssv.AttesterValueCheckF(km, networkConfig.Beacon.GetBeaconNetwork(), consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes))(data))
		require.NoError(t, vc.AttesterValueCheck(ssv.AttesterValueCheckF(km, networkConfig.Beacon.GetBeaconNetwork(), consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes))(data))

		require.ErrorAs(t, ssv.AttesterValueCheckF(km, networkConfig.Beacon.GetBeaconNetwork(), consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes)(data), &ekm.SlashableAttestationError{})
	})

	t.Run("proposer", func(t *testing.T) {
		vc := New(km, sharePKBytes)

		block := testingutils.TestingBeaconBlockCapella
		block.Slot = currentSlot.GetSlot()

		attestationDataBytes, err := block.MarshalSSZ()
		require.NoError(t, err)

		consensusData := &spectypes.ConsensusData{
			Duty: spectypes.Duty{
				Type:                    spectypes.BNRoleProposer,
				PubKey:                  testingutils.TestingValidatorPubKey,
				Slot:                    currentSlot.GetSlot(),
				ValidatorIndex:          testingutils.TestingValidatorIndex,
				CommitteeIndex:          3,
				CommitteesAtSlot:        36,
				CommitteeLength:         128,
				ValidatorCommitteeIndex: 11,
			},
			DataSSZ: attestationDataBytes,
			Version: spec.DataVersionCapella,
		}

		data, err := consensusData.Encode()
		require.NoError(t, err)

		require.NoError(t, vc.ProposerValueCheck(ssv.ProposerValueCheckF(km, networkConfig.Beacon.GetBeaconNetwork(), consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes))(data))
		require.NoError(t, vc.ProposerValueCheck(ssv.ProposerValueCheckF(km, networkConfig.Beacon.GetBeaconNetwork(), consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes))(data))

		require.ErrorAs(t, ssv.ProposerValueCheckF(km, networkConfig.Beacon.GetBeaconNetwork(), consensusData.Duty.PubKey[:], consensusData.Duty.ValidatorIndex, sharePKBytes)(data), &ekm.SlashableProposalError{})
	})
}
