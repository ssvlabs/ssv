package dutytracer

import (
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/traces"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	"github.com/ssvlabs/ssv/registry/storage"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

func TestCollector_RecordsFailedMessages(t *testing.T) {
	tracer := New(zap.NewNop(), registrystoragemocks.NewMockValidatorStore(gomock.NewController(t)), nil, new(mockDutyTraceStore), networkconfig.TestNetwork.Beacon, nil, nil)
	identifier := ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), spectypes.RoleCommittee)
	attrs := []attribute.KeyValue{
		observability.ValidatorMsgTypeAttribute(spectypes.SSVPartialSignatureMsgType),
		observability.RunnerRoleAttribute(spectypes.RoleCommittee),
	}
	before := counterValue(t, "ssv.dutytracer.messages.failed", attrs...)

	// A partial-signature message without any signatures can't be traced.
	data, err := (&spectypes.PartialSignatureMessages{Slot: 1}).Encode()
	require.NoError(t, err)
	require.Error(t, tracer.Collect(t.Context(), buildPartialSigMessage(identifier, data), dummyVerify))

	assert.Equal(t, before+1, counterValue(t, "ssv.dutytracer.messages.failed", attrs...))
}

func TestCollector_RecordsDroppedPendingSignatures(t *testing.T) {
	validators := registrystoragemocks.NewMockValidatorStore(gomock.NewController(t))
	tracer := New(zap.NewNop(), validators, mockclient{}, new(mockDutyTraceStore), networkconfig.TestNetwork.Beacon, nil, nil)

	const slot = phase0.Slot(30)
	identifier := ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])
	committee := &storage.Committee{ID: committeeID, Operators: []spectypes.OperatorID{1, 2, 3, 4}}
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()

	roleAttr := observability.RunnerRoleAttribute(spectypes.RoleCommittee)
	before := counterValue(t, "ssv.dutytracer.pending_signatures.dropped", roleAttr)

	// Post-consensus signatures arrive, but no proposal or decided ever does, so their root stays unknown.
	for _, signer := range []spectypes.OperatorID{1, 2} {
		data := encodePartialSigMessages(slot, 42, signer, phase0.Root{7})
		require.NoError(t, tracer.Collect(t.Context(), buildPartialSigMessage(identifier, data), dummyVerify))
	}
	tracer.dumpCommitteeToDBPeriodically(slot)

	assert.Equal(t, before+2, counterValue(t, "ssv.dutytracer.pending_signatures.dropped", roleAttr))
}

func TestCollector_RecordsLateUnclassifiedSignatures(t *testing.T) {
	const slot = phase0.Slot(32)
	identifier := ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), spectypes.RoleCommittee)
	var committeeID spectypes.CommitteeID
	copy(committeeID[:], identifier.GetDutyExecutorID()[16:])

	validators := registrystoragemocks.NewMockValidatorStore(gomock.NewController(t))
	committee := &storage.Committee{ID: committeeID, Operators: []spectypes.OperatorID{1, 2, 3, 4}}
	validators.EXPECT().Committee(committeeID).Return(committee, true).AnyTimes()
	dutyStore := &mockDutyTraceStore{committeeDutyTrace: &traces.CommitteeDutyTrace{CommitteeID: committeeID, Slot: slot, Role: spectypes.RoleCommittee}}
	tracer := New(zap.NewNop(), validators, mockclient{}, dutyStore, networkconfig.TestNetwork.Beacon, nil, nil)
	tracer.lastEvictedSlot.Store(uint64(slot))

	roleAttr := observability.RunnerRoleAttribute(spectypes.RoleCommittee)
	before := counterValue(t, "ssv.dutytracer.pending_signatures.dropped", roleAttr)

	// The stored trace has no proposal to derive role roots from, so the signature can't be classified.
	data := encodePartialSigMessages(slot, 42, 1, phase0.Root{7})
	require.NoError(t, tracer.Collect(t.Context(), buildPartialSigMessage(identifier, data), dummyVerify))

	assert.Equal(t, before+1, counterValue(t, "ssv.dutytracer.pending_signatures.dropped", roleAttr))
}

func TestCollector_RecordsLostTraces(t *testing.T) {
	tracer := New(zap.NewNop(), registrystoragemocks.NewMockValidatorStore(gomock.NewController(t)), nil, &mockDutyTraceStore{err: errors.New("disk full")}, networkconfig.TestNetwork.Beacon, nil, nil)

	const slot = phase0.Slot(31)
	_, _, err := tracer.getOrCreateCommitteeTrace(slot, spectypes.CommitteeID{1}, spectypes.RoleCommittee)
	require.NoError(t, err)

	kindAttr := attribute.String("ssv.dutytracer.trace.kind", traceKindCommittee)
	before := counterValue(t, "ssv.dutytracer.traces.lost", kindAttr)

	tracer.dumpCommitteeToDBPeriodically(slot)

	assert.Equal(t, before+1, counterValue(t, "ssv.dutytracer.traces.lost", kindAttr))
}
