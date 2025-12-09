package operator

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter/api"
	exporterstore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/networkconfig"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/operator/validator"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

type traceCollectorStub struct {
	entries map[phase0.Slot][]dutytracer.ParticipantsRangeIndexEntry
}

func (t *traceCollectorStub) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return t.entries[slot], nil
}

func (t *traceCollectorStub) GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, dutytracer.ErrNotFound
}

type traceCollectorErrStub struct {
	err error
}

func (t *traceCollectorErrStub) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, t.err
}

func (t *traceCollectorErrStub) GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, t.err
}

type traceCollectorNotFoundStub struct{}

func (t *traceCollectorNotFoundStub) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, dutytracer.ErrNotFound
}

func (t *traceCollectorNotFoundStub) GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, dutytracer.ErrNotFound
}

type traceCollectorStoreNotFoundStub struct{}

func (t *traceCollectorStoreNotFoundStub) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, indices []phase0.ValidatorIndex) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, exporterstore.ErrNotFound
}

func (t *traceCollectorStoreNotFoundStub) GetCommitteeDecideds(slot phase0.Slot, index phase0.ValidatorIndex, roles ...spectypes.BeaconRole) ([]dutytracer.ParticipantsRangeIndexEntry, error) {
	return nil, exporterstore.ErrNotFound
}

func newNodeWithCollector(t *testing.T, collector dutyTraceDecidedsProvider, setupStore func(*registrystoragemocks.MockValidatorStore)) *Node {
	logger := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	store := registrystoragemocks.NewMockValidatorStore(ctrl)
	if setupStore != nil {
		setupStore(store)
	}
	return &Node{
		logger:         logger,
		network:        networkconfig.TestNetwork,
		traceCollector: collector,
		validatorOptions: validator.ControllerOptions{
			ValidatorStore: store,
		},
	}
}

func decidedMessage(from, to uint64, pkHex, role string) *api.NetworkMessage {
	return &api.NetworkMessage{Msg: api.Message{
		Type: api.TypeDecided,
		Filter: api.MessageFilter{
			From:      from,
			To:        to,
			PublicKey: pkHex,
			Role:      role,
		},
	}}
}

func makePK(bytes []byte) spectypes.ValidatorPK {
	var pk spectypes.ValidatorPK
	copy(pk[:], bytes)
	return pk
}

func TestHandleQueryRequests_UsesTraceCollector(t *testing.T) {
	pk := makePK([]byte{1, 2, 3, 4, 5})
	idx := phase0.ValidatorIndex(7)

	collector := &traceCollectorStub{
		entries: map[phase0.Slot][]dutytracer.ParticipantsRangeIndexEntry{
			phase0.Slot(10): {
				{Slot: phase0.Slot(10), Index: idx, Signers: []spectypes.OperatorID{11, 12}},
			},
		},
	}

	node := newNodeWithCollector(t, collector, func(s *registrystoragemocks.MockValidatorStore) {
		s.EXPECT().ValidatorIndex(pk).Return(idx, true).AnyTimes()
	})

	nm := decidedMessage(uint64(10), uint64(10), hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

	node.handleQueryRequests(nm)

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	data, ok := nm.Msg.Data.([]*api.ParticipantsAPI)
	require.True(t, ok, "response data should be participants slice")
	require.Len(t, data, 1)
	require.Equal(t, uint64(10), uint64(data[0].Slot))
	require.Equal(t, []spectypes.OperatorID{11, 12}, data[0].Signers)
}

func TestHandleQueryRequests_ValidatorNotFound(t *testing.T) {
	node := newNodeWithCollector(t, &traceCollectorStub{}, func(s *registrystoragemocks.MockValidatorStore) {
		s.EXPECT().ValidatorIndex(gomock.Any()).Return(phase0.ValidatorIndex(0), false).AnyTimes()
	})

	nm := decidedMessage(1, 1, "abcd", spectypes.BNRoleProposer.String())

	node.handleQueryRequests(nm)

	require.Equal(t, api.TypeError, nm.Msg.Type)
	require.Contains(t, nm.Msg.Data.([]string)[0], "validator not found")
}

func TestHandleDecidedFromTraceCollector_InvalidPubKey(t *testing.T) {
	node := newNodeWithCollector(t, &traceCollectorStub{}, nil)

	nm := decidedMessage(1, 1, "zz-not-hex", spectypes.BNRoleProposer.String())

	node.handleQueryRequests(nm)

	require.Equal(t, api.TypeError, nm.Msg.Type)
	require.Contains(t, nm.Msg.Data.([]string)[0], "invalid publicKey")
}

func TestHandleDecidedFromTraceCollector_InvalidRole(t *testing.T) {
	pk := makePK([]byte{1, 2, 3})
	node := newNodeWithCollector(t, &traceCollectorStub{}, func(s *registrystoragemocks.MockValidatorStore) {
		s.EXPECT().ValidatorIndex(pk).Return(phase0.ValidatorIndex(5), true).AnyTimes()
	})

	nm := decidedMessage(1, 1, hex.EncodeToString(pk[:]), "NOT_A_ROLE")

	node.handleQueryRequests(nm)

	require.Equal(t, api.TypeError, nm.Msg.Type)
	require.Contains(t, nm.Msg.Data.([]string)[0], "role doesn't exist")
}

func TestHandleDecidedFromTraceCollector_CollectorError(t *testing.T) {
	pk := makePK([]byte{1, 2, 3})
	collector := &traceCollectorErrStub{err: errors.New("boom")}

	node := newNodeWithCollector(t, collector, func(s *registrystoragemocks.MockValidatorStore) {
		s.EXPECT().ValidatorIndex(pk).Return(phase0.ValidatorIndex(5), true).AnyTimes()
	})

	nm := decidedMessage(1, 1, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

	node.handleQueryRequests(nm)

	require.Equal(t, api.TypeError, nm.Msg.Type)
	require.Contains(t, nm.Msg.Data.([]string)[0], "internal error - could not build response")
}

func TestHandleDecidedFromTraceCollector_NoMessagesOnNotFound(t *testing.T) {
	pk := makePK([]byte{1, 2, 3, 4})
	idx := phase0.ValidatorIndex(10)

	tests := []struct {
		name      string
		collector dutyTraceDecidedsProvider
	}{
		{
			name:      "dutytracer not found",
			collector: &traceCollectorNotFoundStub{},
		},
		{
			name:      "store not found",
			collector: &traceCollectorStoreNotFoundStub{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := newNodeWithCollector(t, tt.collector, func(s *registrystoragemocks.MockValidatorStore) {
				s.EXPECT().ValidatorIndex(pk).Return(idx, true).AnyTimes()
			})

			nm := decidedMessage(1, 1, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

			node.handleQueryRequests(nm)

			require.Equal(t, api.TypeDecided, nm.Msg.Type)
			errs, ok := nm.Msg.Data.([]string)
			require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
			require.Len(t, errs, 1)
			require.Equal(t, "no messages", errs[0])
		})
	}
}
