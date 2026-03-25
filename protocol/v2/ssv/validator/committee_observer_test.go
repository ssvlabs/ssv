package validator

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/v2/protocol/v2/ssv"
	registrystoragemocks "github.com/ssvlabs/ssv/v2/registry/storage/mocks"
)

func TestCommitteeObserver_VerifySig_MissingValidatorLogsContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	core, recorded := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	const (
		slot          = phase0.Slot(55)
		existingIndex = phase0.ValidatorIndex(10)
		missingIndex  = phase0.ValidatorIndex(11)
		signer        = spectypes.OperatorID(3)
	)

	root := phase0.Root{1, 2, 3}
	validatorStore := registrystoragemocks.NewMockValidatorStore(ctrl)
	validatorStore.EXPECT().ValidatorByIndex(missingIndex).Return(nil, false)

	ncv := &CommitteeObserver{
		msgID:          spectypes.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee),
		logger:         logger,
		ValidatorStore: validatorStore,
		postConsensusContainer: map[phase0.Slot]map[phase0.ValidatorIndex]*ssv.PartialSigContainer{
			slot: {
				existingIndex: ssv.NewPartialSigContainer(3),
			},
		},
	}

	partialMsgs := &spectypes.PartialSignatureMessages{
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				ValidatorIndex: missingIndex,
				Signer:         signer,
				SigningRoot:    root,
			},
		},
	}

	err := ncv.VerifySig(partialMsgs)
	require.EqualError(t, err, fmt.Sprintf("could not find share for validator with index %d", missingIndex))

	logs := recorded.FilterMessage("verify partial sig: validator share not found by index").All()
	require.Len(t, logs, 1)

	fields := logs[0].ContextMap()
	require.EqualValues(t, slot, fields["slot"])
	require.EqualValues(t, signer, fields["operator_id"])
	require.EqualValues(t, missingIndex, fields["validator_index"])
	require.Equal(t, hex.EncodeToString(root[:]), fields["root"])
	require.EqualValues(t, 1, fields["partial_msgs_count"])
	require.EqualValues(t, 1, fields["slot_container_validators"])
	require.EqualValues(t, 1, fields["post_consensus_container_slots"])
	require.Equal(t, false, fields["own_validator"])
}
