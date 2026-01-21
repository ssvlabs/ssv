package operator

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/exporter/api"
	dutytracestore "github.com/ssvlabs/ssv/exporter/store"
	"github.com/ssvlabs/ssv/exporter2"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/operator/validator"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type validatorDutyKey struct {
	slot  phase0.Slot
	role  spectypes.BeaconRole
	index phase0.ValidatorIndex
}

// faultingDutyTraceStore wraps the real duty trace store and can inject errors
// for specific reads. This keeps tests realistic (real DB + SSZ) while still
// letting us assert WS error mapping behavior deterministically.
type faultingDutyTraceStore struct {
	*dutytracestore.DutyTraceStore
	getValidatorDutyErr map[validatorDutyKey]error
}

func newFaultingDutyTraceStore(inner *dutytracestore.DutyTraceStore) *faultingDutyTraceStore {
	return &faultingDutyTraceStore{
		DutyTraceStore:      inner,
		getValidatorDutyErr: make(map[validatorDutyKey]error),
	}
}

func (s *faultingDutyTraceStore) SetGetValidatorDutyError(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex, err error) {
	s.getValidatorDutyErr[validatorDutyKey{slot: slot, role: role, index: index}] = err
}

func (s *faultingDutyTraceStore) GetValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) (*exporter.ValidatorDutyTrace, error) {
	if err := s.getValidatorDutyErr[validatorDutyKey{slot: slot, role: role, index: index}]; err != nil {
		return nil, err
	}
	return s.DutyTraceStore.GetValidatorDuty(slot, role, index)
}

type wsQueryHarness struct {
	t *testing.T

	node *Node

	ctrl          *gomock.Controller
	validatorMock *registrystoragemocks.MockValidatorStore

	db    basedb.Database
	store *faultingDutyTraceStore
}

func newWSQueryHarness(t *testing.T) *wsQueryHarness {
	t.Helper()

	logger := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	validatorMock := registrystoragemocks.NewMockValidatorStore(ctrl)

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	realStore := dutytracestore.New(db)
	store := newFaultingDutyTraceStore(realStore)

	collector := dutytracer.New(zap.NewNop(), validatorMock, nil, store, networkconfig.TestNetwork.Beacon, nil, nil)
	coreExporter := exporter2.NewExporter(zap.NewNop(), ibftstorage.NewStores(), collector, validatorMock)

	node := &Node{
		logger:       logger,
		network:      networkconfig.TestNetwork,
		exporterRead: coreExporter,
		validatorOptions: validator.ControllerOptions{
			ValidatorStore: validatorMock,
		},
	}

	return &wsQueryHarness{
		t:             t,
		node:          node,
		ctrl:          ctrl,
		validatorMock: validatorMock,
		db:            db,
		store:         store,
	}
}

func (h *wsQueryHarness) ExpectValidator(pk spectypes.ValidatorPK, idx phase0.ValidatorIndex, found bool) {
	h.t.Helper()
	h.validatorMock.EXPECT().ValidatorIndex(pk).Return(idx, found).AnyTimes()
	if found {
		h.validatorMock.EXPECT().ValidatorPubkey(idx).Return(pk, true).AnyTimes()
	}
}

func (h *wsQueryHarness) SaveValidatorDuty(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex, decidedsSigners []spectypes.OperatorID) {
	h.t.Helper()
	duty := &exporter.ValidatorDutyTrace{
		ConsensusTrace: exporter.ConsensusTrace{
			Decideds: []*exporter.DecidedTrace{
				{Signers: decidedsSigners},
			},
		},
		Slot:      slot,
		Role:      role,
		Validator: index,
	}
	require.NoError(h.t, h.store.SaveValidatorDuty(duty))
}

func (h *wsQueryHarness) SaveValidatorDutyNoSigners(slot phase0.Slot, role spectypes.BeaconRole, index phase0.ValidatorIndex) {
	h.t.Helper()
	duty := &exporter.ValidatorDutyTrace{
		ConsensusTrace: exporter.ConsensusTrace{},
		Slot:           slot,
		Role:           role,
		Validator:      index,
	}
	require.NoError(h.t, h.store.SaveValidatorDuty(duty))
}

func (h *wsQueryHarness) SaveCommitteeDutyAttester(slot phase0.Slot, validatorIndex phase0.ValidatorIndex, committeeID spectypes.CommitteeID, signers []spectypes.OperatorID) {
	h.t.Helper()
	require.NoError(h.t, h.store.SaveCommitteeDutyLink(slot, validatorIndex, committeeID))
	duty := &exporter.CommitteeDutyTrace{
		ConsensusTrace: exporter.ConsensusTrace{},
		Slot:           slot,
		CommitteeID:    committeeID,
		Attester:       make([]*exporter.SignerData, 0, len(signers)),
	}
	for _, s := range signers {
		duty.Attester = append(duty.Attester, &exporter.SignerData{Signer: s})
	}
	require.NoError(h.t, h.store.SaveCommitteeDuty(duty))
}

func (h *wsQueryHarness) QueryDecided(from, to uint64, pkHex, role string) *api.NetworkMessage {
	h.t.Helper()
	nm := decidedMessage(from, to, pkHex, role)
	h.node.handleQueryRequests(nm)
	return nm
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

func TestWSQuery_Decided_Proposer_SingleSlot(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{1, 2, 3, 4, 5})
	idx := phase0.ValidatorIndex(7)

	h.ExpectValidator(pk, idx, true)
	h.SaveValidatorDuty(phase0.Slot(10), spectypes.BNRoleProposer, idx, []spectypes.OperatorID{11, 12})

	nm := h.QueryDecided(uint64(10), uint64(10), hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())
	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	require.Equal(t, nm.Msg.Filter.PublicKey, hex.EncodeToString(pk[:]))
	data, ok := nm.Msg.Data.([]*api.ParticipantsAPI)
	require.True(t, ok, "response data should be participants slice")
	require.Len(t, data, 1)
	require.Equal(t, uint64(10), uint64(data[0].Slot))
	require.Equal(t, []spectypes.OperatorID{11, 12}, data[0].Signers)
	require.Equal(t, spectypes.BNRoleProposer.String(), data[0].Role)
	require.Equal(t, hex.EncodeToString(pk[:]), data[0].ValidatorPK)
}

func TestWSQuery_Decided_Attester_UsesCommitteeLookup(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{9, 9, 9, 9})
	idx := phase0.ValidatorIndex(42)
	committeeID := spectypes.CommitteeID{1}

	h.ExpectValidator(pk, idx, true)
	h.SaveCommitteeDutyAttester(phase0.Slot(3), idx, committeeID, []spectypes.OperatorID{1, 2, 3})

	nm := h.QueryDecided(3, 3, hex.EncodeToString(pk[:]), spectypes.BNRoleAttester.String())

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	data, ok := nm.Msg.Data.([]*api.ParticipantsAPI)
	require.True(t, ok, "response data should be participants slice")
	require.Len(t, data, 1)
	require.Equal(t, uint64(3), uint64(data[0].Slot))
	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, data[0].Signers)
	require.Equal(t, spectypes.BNRoleAttester.String(), data[0].Role)
}

func TestWSQuery_Decided_RangeMultipleSlots(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{8, 7, 6})
	idx := phase0.ValidatorIndex(5)

	h.ExpectValidator(pk, idx, true)
	h.SaveValidatorDuty(phase0.Slot(10), spectypes.BNRoleProposer, idx, []spectypes.OperatorID{11})
	h.SaveValidatorDuty(phase0.Slot(11), spectypes.BNRoleProposer, idx, []spectypes.OperatorID{12})
	h.SaveValidatorDuty(phase0.Slot(12), spectypes.BNRoleProposer, idx, []spectypes.OperatorID{13})

	nm := h.QueryDecided(10, 12, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	data, ok := nm.Msg.Data.([]*api.ParticipantsAPI)
	require.True(t, ok, "response data should be participants slice")
	require.Len(t, data, 3)
	require.Equal(t, uint64(10), uint64(data[0].Slot))
	require.Equal(t, uint64(11), uint64(data[1].Slot))
	require.Equal(t, uint64(12), uint64(data[2].Slot))
}

func TestWSQuery_InvalidPubKeyHex_ReturnsTypeError(t *testing.T) {
	h := newWSQueryHarness(t)
	nm := h.QueryDecided(1, 1, "zz-not-hex", spectypes.BNRoleProposer.String())
	require.Equal(t, api.TypeError, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0], "invalid publicKey")
}

func TestWSQuery_ValidatorNotFound_ReturnsTypeError(t *testing.T) {
	h := newWSQueryHarness(t)
	pkBytes, err := hex.DecodeString("abcd")
	require.NoError(t, err)
	pk := makePK(pkBytes)

	h.ExpectValidator(pk, 0, false)

	nm := h.QueryDecided(1, 1, "abcd", spectypes.BNRoleProposer.String())
	require.Equal(t, api.TypeError, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 1)
	require.Equal(t, "validator not found for public key abcd", errs[0])
}

func TestWSQuery_InvalidRole_ReturnsTypeError(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{1, 2, 3})
	idx := phase0.ValidatorIndex(5)

	h.ExpectValidator(pk, idx, true)

	nm := h.QueryDecided(1, 1, hex.EncodeToString(pk[:]), "NOT_A_ROLE")

	require.Equal(t, api.TypeError, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 1)
	require.Equal(t, `role doesn't exist: "NOT_A_ROLE"`, errs[0])
}

func TestWSQuery_TraceStoreError_NoEntries_ReturnsInternalError(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{1, 2, 3})
	idx := phase0.ValidatorIndex(5)

	h.ExpectValidator(pk, idx, true)
	h.store.SetGetValidatorDutyError(phase0.Slot(1), spectypes.BNRoleProposer, idx, errors.New("boom"))

	nm := h.QueryDecided(1, 1, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())
	require.Equal(t, api.TypeError, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0], "internal error - could not build response")
	require.Contains(t, errs[0], "boom")
}

func TestWSQuery_NoMessagesOnNotFound(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{1, 2, 3, 4})
	idx := phase0.ValidatorIndex(10)

	h.ExpectValidator(pk, idx, true)

	nm := h.QueryDecided(1, 1, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 1)
	require.Equal(t, "no messages", errs[0])
}

func TestWSQuery_Decided_Attester_NoMessagesWhenCommitteeNotFound(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{7, 7, 7, 7})
	idx := phase0.ValidatorIndex(99)

	h.ExpectValidator(pk, idx, true)

	// No committee duty link and no committee duty saved -> treated as "no messages".
	nm := h.QueryDecided(3, 3, hex.EncodeToString(pk[:]), spectypes.BNRoleAttester.String())

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 1)
	require.Equal(t, "no messages", errs[0])
}

func TestWSQuery_ErrorOnOneSlotButEntriesOnAnother_StillReturnsEntries(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{1, 2, 3, 4})
	idx := phase0.ValidatorIndex(10)

	h.ExpectValidator(pk, idx, true)
	h.SaveValidatorDuty(phase0.Slot(10), spectypes.BNRoleProposer, idx, []spectypes.OperatorID{1})
	h.store.SetGetValidatorDutyError(phase0.Slot(11), spectypes.BNRoleProposer, idx, errors.New("boom"))

	nm := h.QueryDecided(10, 11, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	data, ok := nm.Msg.Data.([]*api.ParticipantsAPI)
	require.True(t, ok, "response data should be participants slice")
	require.Len(t, data, 1)
	require.Equal(t, uint64(10), uint64(data[0].Slot))
}

func TestWSQuery_NetworkMessageParseError_GoesThroughErrorHandler(t *testing.T) {
	h := newWSQueryHarness(t)

	nm := &api.NetworkMessage{Err: errors.New("parsefail")}
	h.node.handleQueryRequests(nm)

	require.Equal(t, api.TypeError, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 2)
	require.Equal(t, "could not parse network message: parsefail", errs[0])
	require.Equal(t, "parsefail", errs[1])
}

func TestWSQuery_UnknownMessageType_ReturnsBadRequest(t *testing.T) {
	h := newWSQueryHarness(t)

	nm := &api.NetworkMessage{Msg: api.Message{
		Type:   "banana",
		Filter: api.MessageFilter{From: 1, To: 1},
	}}
	h.node.handleQueryRequests(nm)

	require.Equal(t, api.TypeError, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 1)
	require.Equal(t, "bad request - unknown message type 'banana'", errs[0])
}

func TestWSQuery_FromGreaterThanTo_ReturnsNoMessages(t *testing.T) {
	h := newWSQueryHarness(t)
	pk := makePK([]byte{1, 2, 3, 4})
	idx := phase0.ValidatorIndex(10)

	h.ExpectValidator(pk, idx, true)

	nm := h.QueryDecided(11, 10, hex.EncodeToString(pk[:]), spectypes.BNRoleProposer.String())

	require.Equal(t, api.TypeDecided, nm.Msg.Type)
	errs, ok := nm.Msg.Data.([]string)
	require.True(t, ok, "expected []string, got %#v", nm.Msg.Data)
	require.Len(t, errs, 1)
	require.Equal(t, "no messages", errs[0])
}
