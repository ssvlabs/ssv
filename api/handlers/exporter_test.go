package handlers

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/api"
	exportertypes "github.com/ssvlabs/ssv/exporter"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	dutytracer "github.com/ssvlabs/ssv/operator/dutytracer"
	"github.com/ssvlabs/ssv/operator/slotticker"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
)

// mockParticipantStore is a basic mock for qbftstorage.ParticipantStore.
type mockParticipantStore struct {
	participantsRangeEntries map[string][]qbftstorage.ParticipantsRangeEntry
}

// newMockParticipantStore creates a new instance of mockParticipantStore.
func newMockParticipantStore() *mockParticipantStore {
	return &mockParticipantStore{
		participantsRangeEntries: make(map[string][]qbftstorage.ParticipantsRangeEntry),
	}
}

func (m *mockParticipantStore) CleanAllInstances() error {
	return nil
}

func (m *mockParticipantStore) SaveParticipants(spectypes.ValidatorPK, phase0.Slot, []spectypes.OperatorID) (bool, error) {
	return true, nil
}

// GetAllParticipantsInRange returns all participant entries within the given slot range.
func (m *mockParticipantStore) GetAllParticipantsInRange(from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	var result []qbftstorage.ParticipantsRangeEntry
	for _, entries := range m.participantsRangeEntries {
		for _, entry := range entries {
			if entry.Slot >= from && entry.Slot <= to {
				result = append(result, entry)
			}
		}
	}
	return result, nil
}

// GetParticipantsInRange returns participant entries for a given public key and slot range.
func (m *mockParticipantStore) GetParticipantsInRange(pk spectypes.ValidatorPK, from, to phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	key := hex.EncodeToString(pk[:])
	var result []qbftstorage.ParticipantsRangeEntry
	entries, ok := m.participantsRangeEntries[key]
	if !ok {
		return result, nil
	}
	for _, entry := range entries {
		if entry.Slot >= from && entry.Slot <= to {
			result = append(result, entry)
		}
	}
	return result, nil
}

func (m *mockParticipantStore) GetParticipants(spectypes.ValidatorPK, phase0.Slot) ([]spectypes.OperatorID, error) {
	return nil, nil
}

func (m *mockParticipantStore) Prune(context.Context, phase0.Slot) {
	// no-op.
}

func (m *mockParticipantStore) PruneContinously(context.Context, slotticker.Provider, phase0.Slot) {
	// no-op.
}

// AddEntry adds an entry to the mock store.
func (m *mockParticipantStore) AddEntry(pk spectypes.ValidatorPK, slot phase0.Slot, signers []uint64) {
	key := hex.EncodeToString(pk[:])
	entry := qbftstorage.ParticipantsRangeEntry{
		Slot:    slot,
		PubKey:  pk,
		Signers: signers,
	}
	m.participantsRangeEntries[key] = append(m.participantsRangeEntries[key], entry)
}

// errorAllRangeMockStore forces an error on GetAllParticipantsInRange.
type errorAllRangeMockStore struct {
	*mockParticipantStore
}

func (m *errorAllRangeMockStore) GetAllParticipantsInRange(phase0.Slot, phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	return nil, fmt.Errorf("forced error on GetAllParticipantsInRange")
}

// errorByPKMockStore forces an error on GetParticipantsInRange.
type errorByPKMockStore struct {
	*mockParticipantStore
}

func (m *errorByPKMockStore) GetParticipantsInRange(spectypes.ValidatorPK, phase0.Slot, phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	return nil, fmt.Errorf("forced error on GetParticipantsInRange")
}

// TestTransformToParticipantResponse verifies mapping from storage entry to API response.
func TestTransformToParticipantResponse(t *testing.T) {
	t.Parallel()

	// create test entry with a realistic public key.
	pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")

	var pk spectypes.ValidatorPK
	copy(pk[:], pkBytes)

	entry := qbftstorage.ParticipantsRangeEntry{
		Slot:    phase0.Slot(123),
		PubKey:  pk,
		Signers: []uint64{1, 2, 3, 4},
	}
	role := spectypes.BNRoleAttester
	resp := transformToParticipantResponse(role, entry)

	assert.Equal(t, role.String(), resp.Role)
	assert.Equal(t, uint64(123), resp.Slot)
	assert.Equal(t, hex.EncodeToString(pk[:]), resp.PublicKey)
	assert.Equal(t, []uint64{1, 2, 3, 4}, resp.Message.Signers)
}

// TestExporterDecideds runs table-driven tests for the Decideds handler.
func TestExporterDecideds(t *testing.T) {
	tests := []struct {
		name           string
		request        map[string]interface{}
		setupMock      func(*mockParticipantStore)
		expectedStatus int
		validateResp   func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "valid request - roles & slot range",
			request: map[string]interface{}{
				"from":  100,
				"to":    200,
				"roles": []string{"ATTESTER"},
			},
			setupMock: func(store *mockParticipantStore) {
				// add entries for two keys in different slots.
				pk1Bytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				pk2Bytes := common.Hex2Bytes("824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170")

				var pk1, pk2 spectypes.ValidatorPK
				copy(pk1[:], pk1Bytes)
				copy(pk2[:], pk2Bytes)

				store.AddEntry(pk1, phase0.Slot(100), []uint64{1, 2, 3})
				store.AddEntry(pk1, phase0.Slot(150), []uint64{1, 2, 3})
				store.AddEntry(pk2, phase0.Slot(180), []uint64{4, 5, 6})
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}

				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 3)

				for _, item := range resp.Data {
					assert.Equal(t, "ATTESTER", item.Role)
				}

				slots := map[uint64]bool{}
				for _, item := range resp.Data {
					slots[item.Slot] = true
				}

				require.True(t, slots[100])
				require.True(t, slots[150])
				require.True(t, slots[180])
			},
		},
		{
			name: "valid request - pubkeys filter",
			request: map[string]interface{}{
				"from":    100,
				"to":      200,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockParticipantStore) {
				// add entries for two keys; only one should match the filter.
				pk1Bytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				pk2Bytes := common.Hex2Bytes("824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170")

				var pk1, pk2 spectypes.ValidatorPK
				copy(pk1[:], pk1Bytes)
				copy(pk2[:], pk2Bytes)

				store.AddEntry(pk1, phase0.Slot(100), []uint64{1, 2, 3})
				store.AddEntry(pk1, phase0.Slot(150), []uint64{1, 2, 3})
				store.AddEntry(pk2, phase0.Slot(180), []uint64{4, 5, 6})
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}

				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				// expect only entries for the filtered pubkey.
				require.Len(t, resp.Data, 2)

				for _, item := range resp.Data {
					assert.Equal(t, "ATTESTER", item.Role)
					assert.Equal(t, "b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1", item.PublicKey)
				}
			},
		},
		{
			name: "invalid request - from > to",
			request: map[string]interface{}{
				"from":  200,
				"to":    100,
				"roles": []string{"ATTESTER"},
			},
			setupMock:      func(store *mockParticipantStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusBadRequest, rec.Code)

				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}

				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Equal(t, "'from' must be less than or equal to 'to'", resp.Message)
			},
		},
		{
			name: "invalid request - no roles",
			request: map[string]interface{}{
				"from": 100,
				"to":   200,
			},
			setupMock:      func(store *mockParticipantStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusBadRequest, rec.Code)

				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}

				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Equal(t, "at least one role is required", resp.Message)
			},
		},
		{
			name: "multiple roles",
			request: map[string]interface{}{
				"from":  100,
				"to":    200,
				"roles": []string{"ATTESTER", "PROPOSER"},
			},
			setupMock: func(store *mockParticipantStore) {
				// add a single entry to be used for both roles.
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)
				store.AddEntry(pk, phase0.Slot(150), []uint64{1, 2, 3})
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}

				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 2)

				roles := map[string]bool{}
				for _, item := range resp.Data {
					roles[item.Role] = true
				}

				require.True(t, roles["ATTESTER"])
				require.True(t, roles["PROPOSER"])
			},
		},
		{
			name: "invalid request - invalid pubkey length",
			request: map[string]interface{}{
				"from":    100,
				"to":      200,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"0x1234"},
			},
			setupMock:      func(store *mockParticipantStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusBadRequest, rec.Code)

				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}

				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Contains(t, resp.Message, "invalid pubkey length at index 0")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set up two mock stores for different roles.
			attesterStore := newMockParticipantStore()
			proposerStore := newMockParticipantStore()

			tt.setupMock(attesterStore)
			tt.setupMock(proposerStore)

			stores := ibftstorage.NewStores()
			stores.Add(spectypes.BNRoleAttester, attesterStore)
			stores.Add(spectypes.BNRoleProposer, proposerStore)

			exporter := NewExporter(zap.NewNop(), stores, nil, nil)

			reqBody, err := json.Marshal(tt.request)

			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/decideds", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			err = exporter.Decideds(rec, req)
			if err != nil {
				rec.Code = tt.expectedStatus
				errorResp := map[string]string{
					"status": http.StatusText(tt.expectedStatus),
					"error":  err.Error(),
				}
				jsonResp, _ := json.Marshal(errorResp)
				rec.Body.Write(jsonResp)
				rec.Header().Set("Content-Type", "application/json")
			}
			tt.validateResp(t, rec)
		})
	}
}

// TestExporterDecideds_InvalidJSON verifies that invalid JSON triggers a binding error.
func TestExporterDecideds_InvalidJSON(t *testing.T) {
	store := newMockParticipantStore()
	stores := ibftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, store)

	exporter := NewExporter(zap.NewNop(), stores, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/decideds", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err := exporter.Decideds(rec, req)
	if err != nil {
		rec.Code = http.StatusBadRequest
		errorResp := map[string]string{
			"status": http.StatusText(http.StatusBadRequest),
			"error":  err.Error(),
		}
		jsonResp, _ := json.Marshal(errorResp)
		rec.Body.Write(jsonResp)
		rec.Header().Set("Content-Type", "application/json")
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "invalid character")
}

// TestExporterDecideds_ErrorGetAllParticipantsInRange tests error handling when GetAllParticipantsInRange fails.
func TestExporterDecideds_ErrorGetAllParticipantsInRange(t *testing.T) {
	store := &errorAllRangeMockStore{newMockParticipantStore()}
	stores := ibftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, store)

	exporter := NewExporter(zap.NewNop(), stores, nil, nil)
	reqData := map[string]interface{}{
		"from":  100,
		"to":    200,
		"roles": []string{"ATTESTER"},
	}
	reqBody, err := json.Marshal(reqData)

	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/decideds", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err = exporter.Decideds(rec, req)

	if err != nil {
		rec.Code = http.StatusInternalServerError
		errorResp := map[string]string{
			"status": http.StatusText(http.StatusInternalServerError),
			"error":  err.Error(),
		}
		jsonResp, _ := json.Marshal(errorResp)
		rec.Body.Write(jsonResp)
		rec.Header().Set("Content-Type", "application/json")
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "error getting participants")
	require.Contains(t, resp.Message, "forced error on GetAllParticipantsInRange")
}

// TestExporterDecideds_ErrorGetParticipantsInRange tests error handling when GetParticipantsInRange fails.
func TestExporterDecideds_ErrorGetParticipantsInRange(t *testing.T) {
	store := &errorByPKMockStore{newMockParticipantStore()}
	stores := ibftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, store)

	exporter := NewExporter(zap.NewNop(), stores, nil, nil)

	reqData := map[string]interface{}{
		"from":    100,
		"to":      200,
		"roles":   []string{"ATTESTER"},
		"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
	}
	reqBody, err := json.Marshal(reqData)

	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/decideds", strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err = exporter.Decideds(rec, req)
	if err != nil {
		rec.Code = http.StatusInternalServerError
		errorResp := map[string]string{
			"status": http.StatusText(http.StatusInternalServerError),
			"error":  err.Error(),
		}
		jsonResp, _ := json.Marshal(errorResp)
		rec.Body.Write(jsonResp)
		rec.Header().Set("Content-Type", "application/json")
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "error getting participants")
	require.Contains(t, resp.Message, "forced error on GetParticipantsInRange")
}

// duty trace tests

// mockTraceStore is a mock implementation of DutyTraceStore
type mockTraceStore struct {
	validatorDecideds           map[string][]qbftstorage.ParticipantsRangeEntry
	committeeDecideds           map[string][]qbftstorage.ParticipantsRangeEntry
	GetValidatorDutyFunc        func(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error)
	GetValidatorDutiesFunc      func(role spectypes.BeaconRole, slot phase0.Slot) ([]*dutytracer.ValidatorDutyTrace, error)
	GetCommitteeDutyFunc        func(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exportertypes.CommitteeDutyTrace, error)
	GetCommitteeDutiesFunc      func(slot phase0.Slot) ([]*exportertypes.CommitteeDutyTrace, error)
	GetCommitteeIDFunc          func(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error)
	GetValidatorDecidedsFunc    func(role spectypes.BeaconRole, slot phase0.Slot, pubKeys []spectypes.ValidatorPK) ([]qbftstorage.ParticipantsRangeEntry, error)
	GetCommitteeDecidedsFunc    func(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error)
	GetAllCommitteeDecidedsFunc func(slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error)
	GetAllValidatorDecidedsFunc func(role spectypes.BeaconRole, slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error)
}

func newMockTraceStore() *mockTraceStore {
	return &mockTraceStore{
		validatorDecideds: make(map[string][]qbftstorage.ParticipantsRangeEntry),
		committeeDecideds: make(map[string][]qbftstorage.ParticipantsRangeEntry),
	}
}

func (m *mockTraceStore) GetValidatorDuty(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error) {
	if m.GetValidatorDutyFunc != nil {
		return m.GetValidatorDutyFunc(role, slot, pubkey)
	}
	return nil, nil
}

func (m *mockTraceStore) GetValidatorDuties(role spectypes.BeaconRole, slot phase0.Slot) ([]*dutytracer.ValidatorDutyTrace, error) {
	if m.GetValidatorDutiesFunc != nil {
		return m.GetValidatorDutiesFunc(role, slot)
	}
	return nil, nil
}

func (m *mockTraceStore) GetCommitteeDuty(slot phase0.Slot, committeeID spectypes.CommitteeID, role ...spectypes.BeaconRole) (*exportertypes.CommitteeDutyTrace, error) {
	if m.GetCommitteeDutyFunc != nil {
		return m.GetCommitteeDutyFunc(slot, committeeID)
	}
	return nil, nil
}

func (m *mockTraceStore) GetCommitteeDuties(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]*exportertypes.CommitteeDutyTrace, error) {
	if m.GetCommitteeDutiesFunc != nil {
		return m.GetCommitteeDutiesFunc(slot)
	}
	return nil, nil
}

func (m *mockTraceStore) GetCommitteeID(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error) {
	if m.GetCommitteeIDFunc != nil {
		return m.GetCommitteeIDFunc(slot, pubkey)
	}
	return spectypes.CommitteeID{}, 0, nil
}

func (m *mockTraceStore) GetValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot, pubKeys []spectypes.ValidatorPK) ([]qbftstorage.ParticipantsRangeEntry, error) {
	if m.GetValidatorDecidedsFunc != nil {
		return m.GetValidatorDecidedsFunc(role, slot, pubKeys)
	}
	key := fmt.Sprintf("%d-%d", role, slot)
	return m.validatorDecideds[key], nil
}

func (m *mockTraceStore) GetAllValidatorDecideds(role spectypes.BeaconRole, slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
	if m.GetAllValidatorDecidedsFunc != nil {
		return m.GetAllValidatorDecidedsFunc(role, slot)
	}
	key := fmt.Sprintf("%d-%d", role, slot)
	return m.validatorDecideds[key], nil
}

func (m *mockTraceStore) GetCommitteeDecideds(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
	if m.GetCommitteeDecidedsFunc != nil {
		return m.GetCommitteeDecidedsFunc(slot, pubKey)
	}
	return nil, nil
}

func (m *mockTraceStore) GetAllCommitteeDecideds(slot phase0.Slot, roles ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
	if m.GetAllCommitteeDecidedsFunc != nil {
		return m.GetAllCommitteeDecidedsFunc(slot)
	}
	key := fmt.Sprintf("%d", slot)
	return m.committeeDecideds[key], nil
}

func (m *mockTraceStore) AddValidatorDecided(role spectypes.BeaconRole, slot phase0.Slot, pubKey spectypes.ValidatorPK, signers []uint64) {
	key := fmt.Sprintf("%d-%d", role, slot)
	entry := qbftstorage.ParticipantsRangeEntry{
		Slot:    slot,
		PubKey:  pubKey,
		Signers: signers,
	}
	m.validatorDecideds[key] = append(m.validatorDecideds[key], entry)
}

func (m *mockTraceStore) AddCommitteeDecided(slot phase0.Slot, pubKey spectypes.ValidatorPK, signers []uint64) {
	key := fmt.Sprintf("%d-%s", slot, hex.EncodeToString(pubKey[:]))
	entry := qbftstorage.ParticipantsRangeEntry{
		Slot:    slot,
		PubKey:  pubKey,
		Signers: signers,
	}
	m.committeeDecideds[key] = append(m.committeeDecideds[key], entry)
}

// TestExporterTraceDecideds runs table-driven tests for the TraceDecideds handler
func TestExporterTraceDecideds(t *testing.T) {
	tests := []struct {
		name           string
		request        map[string]any
		setupMock      func(*mockTraceStore)
		expectedStatus int
		validateResp   func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "valid request - validator decideds",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)
				store.AddValidatorDecided(spectypes.BNRoleProposer, phase0.Slot(150), pk, []uint64{1, 2, 3})
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, "PROPOSER", resp.Data[0].Role)
				assert.Equal(t, uint64(150), resp.Data[0].Slot)
				assert.Equal(t, []uint64{1, 2, 3}, resp.Data[0].Message.Signers)
			},
		},
		{
			name: "valid request - committee decideds",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				// Mock GetCommitteeDecideds to return entries for the requested pubkey
				store.GetCommitteeDecidedsFunc = func(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk,
							Signers: []uint64{1, 2, 3},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, "ATTESTER", resp.Data[0].Role)
				assert.Equal(t, uint64(100), resp.Data[0].Slot)
				assert.Equal(t, []uint64{1, 2, 3}, resp.Data[0].Message.Signers)
			},
		},
		{
			name: "valid request - empty signers array - proposer role",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				entry := qbftstorage.ParticipantsRangeEntry{
					Signers: []uint64{},
				}
				store.AddValidatorDecided(spectypes.BNRoleProposer, phase0.Slot(150), pk, entry.Signers)
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Empty signers array should be filtered out
			},
		},
		{
			name: "valid request - empty signers array - attester role",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				entry := qbftstorage.ParticipantsRangeEntry{
					Signers: []uint64{},
				}
				store.AddCommitteeDecided(phase0.Slot(150), pk, entry.Signers)
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Empty signers array should be filtered out
			},
		},
		{
			name: "invalid request - invalid pubkey length",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"PROPOSER"},
				"pubkeys": api.HexSlice{api.Hex("0x123")}, // malformed hex - too short for a pubkey
			},
			setupMock:      func(store *mockTraceStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Contains(t, resp.Message, "invalid pubkey length: 0x123")
			},
		}, {
			name: "invalid request - invalid range",
			request: map[string]any{
				"from":    100,
				"to":      99,
				"roles":   []string{"PROPOSER"},
				"pubkeys": api.HexSlice{api.Hex("0x123")},
			},
			setupMock:      func(store *mockTraceStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "'from' must be less than or equal to 'to'", resp.Message)
			},
		},
		{
			name: "valid request - all duties in slot range",
			request: map[string]any{
				"from":  100,
				"to":    101,
				"roles": []string{"ATTESTER", "PROPOSER"},
			},
			setupMock: func(store *mockTraceStore) {
				// Mock GetAllValidatorDecideds to return entries for multiple validators
				pk1Bytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				pk2Bytes := common.Hex2Bytes("824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170")

				var pk1, pk2 spectypes.ValidatorPK
				copy(pk1[:], pk1Bytes)
				copy(pk2[:], pk2Bytes)
				store.GetAllValidatorDecidedsFunc = func(role spectypes.BeaconRole, slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk1,
							Signers: []uint64{1, 2, 3},
						},
						{
							Slot:    slot,
							PubKey:  pk2,
							Signers: []uint64{4, 5, 6},
						},
					}, nil
				}
				store.GetAllCommitteeDecidedsFunc = func(slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk1,
							Signers: []uint64{1, 2, 3},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 6) // 2 validators * 2 roles + 1 committee * 2 roles

				// Verify we have entries for both roles
				roles := map[string]bool{}
				for _, item := range resp.Data {
					roles[item.Role] = true
				}
				assert.True(t, roles["ATTESTER"])
				assert.True(t, roles["PROPOSER"])

				// Verify we have entries for both validators
				pubkeys := map[string]bool{}
				for _, item := range resp.Data {
					pubkeys[item.PublicKey] = true
				}
				assert.True(t, pubkeys["b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"])
				assert.True(t, pubkeys["824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"])
			},
		},
		{
			name: "valid request - only duties for provided pubkeys in slot range",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"SYNC_COMMITTEE", "PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				pk1Bytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				pk2Bytes := common.Hex2Bytes("824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170")

				var pk1, pk2 spectypes.ValidatorPK
				copy(pk1[:], pk1Bytes)
				copy(pk2[:], pk2Bytes)

				// Mock GetValidatorDecideds to return entries only for the requested pubkey
				store.GetValidatorDecidedsFunc = func(role spectypes.BeaconRole, slot phase0.Slot, pubKeys []spectypes.ValidatorPK) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk1,
							Signers: []uint64{1, 2, 3},
						},
					}, nil
				}

				// Mock GetCommitteeDecideds to return entries only for the requested pubkey
				store.GetCommitteeDecidedsFunc = func(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk1,
							Signers: []uint64{1, 2, 3},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 2) // 1 validator * 2 roles + 1 committee * 1 role

				// Verify we have entries for both roles
				roles := map[string]bool{}
				for _, item := range resp.Data {
					roles[item.Role] = true
				}
				assert.True(t, roles["SYNC_COMMITTEE"])
				assert.True(t, roles["PROPOSER"])

				// Verify we only have entries for the requested pubkey
				pubkeys := map[string]bool{}
				for _, item := range resp.Data {
					pubkeys[item.PublicKey] = true
				}
				assert.True(t, pubkeys["b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"])
				assert.False(t, pubkeys["824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170"])
			},
		},
		{
			name: "error case - GetAllCommitteeDecideds returns error",
			request: map[string]any{
				"from":  100,
				"to":    100,
				"roles": []string{"ATTESTER"},
			},
			setupMock: func(store *mockTraceStore) {
				store.GetAllCommitteeDecidedsFunc = func(slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return nil, fmt.Errorf("forced error on GetAllCommitteeDecideds")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - GetCommitteeDecideds returns error",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				store.GetCommitteeDecidedsFunc = func(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return nil, fmt.Errorf("forced error on GetCommitteeDecideds")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - GetCommitteeDecideds returns NotFounderror",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				store.GetCommitteeDecidedsFunc = func(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return nil, dutytracer.ErrNotFound
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - GetAllValidatorDecideds returns error",
			request: map[string]any{
				"from":  100,
				"to":    100,
				"roles": []string{"PROPOSER"},
			},
			setupMock: func(store *mockTraceStore) {
				store.GetAllValidatorDecidedsFunc = func(role spectypes.BeaconRole, slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return nil, fmt.Errorf("forced error on GetAllValidatorDecideds")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - GetValidatorDecideds returns error",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				store.GetValidatorDecidedsFunc = func(role spectypes.BeaconRole, slot phase0.Slot, pubKeys []spectypes.ValidatorPK) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return nil, fmt.Errorf("forced error on GetValidatorDecideds")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - from > to",
			request: map[string]any{
				"from":    101,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock:      func(store *mockTraceStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "'from' must be less than or equal to 'to'", resp.Message)
			},
		},
		{
			name: "error case - empty roles",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock:      func(store *mockTraceStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "at least one role is required", resp.Message)
			},
		},
		{
			name: "invalid request - invalid pubkey length",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"0x1234"},
			},
			setupMock:      func(store *mockTraceStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Contains(t, resp.Message, "invalid pubkey length")
			},
		},
		{
			name: "valid request - attester role no request pubkeys GetAllCommitteeDecideds returns no signers",
			request: map[string]any{
				"from":  100,
				"to":    100,
				"roles": []string{"ATTESTER"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)
				store.GetAllCommitteeDecidedsFunc = func(slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk,
							Signers: []uint64{},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0)
			},
		},
		{
			name: "valid request - attester role one pubkey GetCommitteeDecideds returns no signers",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)
				store.GetCommitteeDecidedsFunc = func(slot phase0.Slot, pubKey spectypes.ValidatorPK, _ ...spectypes.BeaconRole) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk,
							Signers: []uint64{},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0)
			},
		},
		{
			name: "valid request - proposer role no pubkey GetAllValidatorDecideds returns no signers",
			request: map[string]any{
				"from":  100,
				"to":    100,
				"roles": []string{"PROPOSER"},
			},
			setupMock: func(store *mockTraceStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)
				store.GetAllValidatorDecidedsFunc = func(role spectypes.BeaconRole, slot phase0.Slot) ([]qbftstorage.ParticipantsRangeEntry, error) {
					return []qbftstorage.ParticipantsRangeEntry{
						{
							Slot:    slot,
							PubKey:  pk,
							Signers: []uint64{},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*ParticipantResponse `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockTraceStore()

			tt.setupMock(store)

			exporter := NewExporter(zap.NewNop(), nil, store, nil)

			reqBody, err := json.Marshal(tt.request)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/traces/decideds", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			err = exporter.TraceDecideds(rec, req)
			if err != nil {
				rec.Code = tt.expectedStatus
				errorResp := map[string]string{
					"status": http.StatusText(tt.expectedStatus),
					"error":  err.Error(),
				}
				jsonResp, _ := json.Marshal(errorResp)
				rec.Body.Write(jsonResp)
				rec.Header().Set("Content-Type", "application/json")
			}
			tt.validateResp(t, rec)
		})
	}
}

func TestExporterTraceDecideds_InvalidJSON(t *testing.T) {
	store := newMockTraceStore()
	exporter := NewExporter(zap.NewNop(), nil, store, nil)
	req := httptest.NewRequest(http.MethodPost, "/traces/decideds", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err := exporter.TraceDecideds(rec, req)
	if err != nil {
		rec.Code = http.StatusBadRequest
		errorResp := map[string]string{
			"status": http.StatusText(http.StatusBadRequest),
			"error":  err.Error(),
		}
		jsonResp, _ := json.Marshal(errorResp)
		rec.Body.Write(jsonResp)
		rec.Header().Set("Content-Type", "application/json")
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "invalid character")
}

func makeCommitteeDutyTrace(slot phase0.Slot) *exportertypes.CommitteeDutyTrace {
	return &exportertypes.CommitteeDutyTrace{
		Slot:        slot,
		CommitteeID: spectypes.CommitteeID{1},
		ConsensusTrace: exportertypes.ConsensusTrace{
			Rounds: []*exportertypes.RoundTrace{
				{
					Proposer: spectypes.OperatorID(1),
					ProposalTrace: &exportertypes.ProposalTrace{
						QBFTTrace: exportertypes.QBFTTrace{
							Round:        1,
							BeaconRoot:   phase0.Root{1},
							Signer:       spectypes.OperatorID(1),
							ReceivedTime: uint64(time.Now().Unix()),
						},
					},
				},
			},
			Decideds: []*exportertypes.DecidedTrace{
				{
					Round:        1,
					BeaconRoot:   phase0.Root{1},
					Signers:      []spectypes.OperatorID{1, 2},
					ReceivedTime: uint64(time.Now().Unix()),
				},
			},
		},
		OperatorIDs: []spectypes.OperatorID{1, 2, 3},
		SyncCommittee: []*exportertypes.SignerData{
			{
				Signer:       spectypes.OperatorID(1),
				ValidatorIdx: []phase0.ValidatorIndex{1},
			},
		},
		ProposalData: []byte{1, 2, 3},
	}
}

// TestExporterCommitteeTraces tests the CommitteeTraces handler
func TestExporterCommitteeTraces(t *testing.T) {
	tests := []struct {
		name            string
		request         map[string]any
		setupMock       func(*mockTraceStore, *mockValidatorStore)
		expectedStatus  int
		validateErrResp func(*testing.T, error)
		validateResp    func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "valid request - all committees",
			request: map[string]any{
				"from": 100,
				"to":   100,
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				store.GetCommitteeDutiesFunc = func(slot phase0.Slot) (traces []*exportertypes.CommitteeDutyTrace, err error) {
					traces = []*exportertypes.CommitteeDutyTrace{
						makeCommitteeDutyTrace(slot),
					}
					return traces, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp committeeTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, uint64(100), resp.Data[0].Slot)
				assert.Len(t, resp.Data[0].Decideds, 1)
				assert.Len(t, resp.Data[0].Consensus, 1)
				assert.Len(t, resp.Data[0].SyncCommittee, 1)
				assert.Len(t, resp.Data[0].SyncCommittee, 1)
				assert.NotEmpty(t, resp.Data[0].Proposal)
			},
		},
		{
			name: "valid request - specific committee IDs",
			request: map[string]any{
				"from":         100,
				"to":           100,
				"committeeIDs": []string{"0eb9655577d1af04ff5d382848be15d1454b04838713bfb1ac209808fe3e9f7f"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				store.GetCommitteeDutyFunc = func(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exportertypes.CommitteeDutyTrace, error) {
					return makeCommitteeDutyTrace(slot), nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp committeeTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, uint64(100), resp.Data[0].Slot)
				assert.Len(t, resp.Data[0].Decideds, 1)
				assert.Len(t, resp.Data[0].Consensus, 1)
				assert.Len(t, resp.Data[0].SyncCommittee, 1)
				assert.NotEmpty(t, resp.Data[0].Proposal)
			},
		},
		{
			name: "invalid request - from > to",
			request: map[string]any{
				"from": 200,
				"to":   100,
			},
			expectedStatus: http.StatusBadRequest,
			validateErrResp: func(t *testing.T, err error) {
				assert.ErrorContains(t, err, "'from' must be less than or equal to 'to'")
			},
		},
		{
			name: "invalid request - invalid committee ID length",
			request: map[string]any{
				"from":         100,
				"to":           200,
				"committeeIDs": api.HexSlice{api.Hex("0x123")},
			},
			expectedStatus: http.StatusBadRequest,
			validateErrResp: func(t *testing.T, err error) {
				assert.ErrorContains(t, err, "invalid committee ID length")
			},
		},
		{
			name: "error case - GetCommitteeDuties returns error",
			request: map[string]any{
				"from": 100,
				"to":   100,
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				store.GetCommitteeDutiesFunc = func(slot phase0.Slot) ([]*exportertypes.CommitteeDutyTrace, error) {
					return nil, fmt.Errorf("forced error on GetCommitteeDuties")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp committeeTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - GetCommitteeDuty returns error",
			request: map[string]any{
				"from":         100,
				"to":           100,
				"committeeIDs": []string{"0eb9655577d1af04ff5d382848be15d1454b04838713bfb1ac209808fe3e9f7f"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				store.GetCommitteeDutyFunc = func(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exportertypes.CommitteeDutyTrace, error) {
					return nil, fmt.Errorf("forced error on GetCommitteeDuty")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp committeeTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockTraceStore()
			validatorStore := newMockValidatorStore()
			if tt.setupMock != nil {
				tt.setupMock(store, validatorStore)
			}

			exporter := NewExporter(zap.NewNop(), nil, store, validatorStore)

			body, err := json.Marshal(tt.request)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/traces/committee", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			err = exporter.CommitteeTraces(rec, req)
			if tt.expectedStatus != http.StatusOK {
				assert.Error(t, err)
				tt.validateErrResp(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, rec.Code)
			tt.validateResp(t, rec)
		})
	}
}

func TestExporterCommitteeTraces_InvalidJSON(t *testing.T) {
	store := newMockTraceStore()
	validatorStore := newMockValidatorStore()
	exporter := NewExporter(zap.NewNop(), nil, store, validatorStore)

	req := httptest.NewRequest(http.MethodPost, "/traces/committee", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err := exporter.CommitteeTraces(rec, req)
	if err != nil {
		rec.Code = http.StatusBadRequest
		errorResp := map[string]string{
			"status": http.StatusText(http.StatusBadRequest),
			"error":  err.Error(),
		}
		jsonResp, _ := json.Marshal(errorResp)
		rec.Body.Write(jsonResp)
		rec.Header().Set("Content-Type", "application/json")
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "invalid character")
}

// TestExporterValidatorTraces runs table-driven tests for the ValidatorTraces handler
func TestExporterValidatorTraces(t *testing.T) {
	tests := []struct {
		name           string
		request        map[string]any
		setupMock      func(*mockTraceStore, *mockValidatorStore)
		expectedStatus int
		validateResp   func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "valid request - by pubkeys",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				store.GetValidatorDutyFunc = func(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error) {
					return &dutytracer.ValidatorDutyTrace{
						ValidatorDutyTrace: exportertypes.ValidatorDutyTrace{
							Slot:      150,
							Role:      role,
							Validator: 1,
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, phase0.Slot(150), resp.Data[0].Slot)
				assert.Equal(t, "PROPOSER", resp.Data[0].Role)
				assert.Equal(t, phase0.ValidatorIndex(1), resp.Data[0].Validator)
			},
		},
		{
			name: "valid request - by indices",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"indices": []uint64{1},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				validatorStore.ValidatorByIndexFunc = func(index phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool) {
					share := &ssvtypes.SSVShare{}
					copy(share.ValidatorPubKey[:], pkBytes)
					return share, true
				}

				store.GetValidatorDutyFunc = func(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error) {
					return &dutytracer.ValidatorDutyTrace{
						ValidatorDutyTrace: exportertypes.ValidatorDutyTrace{
							Slot:      150,
							Role:      role,
							Validator: 1,
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*validatorTrace `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, phase0.Slot(150), resp.Data[0].Slot)
				assert.Equal(t, "PROPOSER", resp.Data[0].Role)
				assert.Equal(t, phase0.ValidatorIndex(1), resp.Data[0].Validator)
			},
		},
		{
			name: "valid request - no pubkeys or indices",
			request: map[string]any{
				"from":  100,
				"to":    100,
				"roles": []string{"PROPOSER"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				validatorStore.ValidatorByIndexFunc = func(index phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool) {
					share := &ssvtypes.SSVShare{}
					copy(share.ValidatorPubKey[:], pkBytes)
					return share, true
				}

				store.GetValidatorDutiesFunc = func(role spectypes.BeaconRole, slot phase0.Slot) ([]*dutytracer.ValidatorDutyTrace, error) {
					results := []*dutytracer.ValidatorDutyTrace{
						{
							ValidatorDutyTrace: exportertypes.ValidatorDutyTrace{
								Slot:      150,
								Role:      role,
								Validator: 1,
							},
						},
						{
							ValidatorDutyTrace: exportertypes.ValidatorDutyTrace{
								Slot:      150,
								Role:      role,
								Validator: 2,
							},
						},
					}
					return results, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Data []*validatorTrace `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 2)
				assert.Equal(t, phase0.Slot(150), resp.Data[0].Slot)
				assert.Equal(t, "PROPOSER", resp.Data[0].Role)
				assert.Equal(t, phase0.ValidatorIndex(1), resp.Data[0].Validator)
				assert.Equal(t, phase0.ValidatorIndex(2), resp.Data[1].Validator)
			},
		},
		{
			name: "invalid request - no pubkeys or indices",
			request: map[string]any{
				"from":  100,
				"to":    200,
				"roles": []string{"ATTESTER"},
			},
			setupMock:      func(store *mockTraceStore, validatorStore *mockValidatorStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "role ATTESTER is a committee duty, please provide either pubkeys or indices to filter the duty for specific a validators subset or use the /committee endpoint to query all the corresponding duties", resp.Message)
			},
		},
		{
			name: "invalid request - validator not found",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"PROPOSER"},
				"indices": []uint64{1},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				validatorStore.ValidatorByIndexFunc = func(index phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool) {
					return nil, false
				}
			},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "validator not found: 1", resp.Message)
			},
		},
		{
			name: "valid request - attester role",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				// Mock GetCommitteeID to return a committee ID and validator index
				store.GetCommitteeIDFunc = func(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error) {
					return spectypes.CommitteeID{1}, 1, nil
				}

				// Mock GetCommitteeDuty to return a committee duty
				store.GetCommitteeDutyFunc = func(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exportertypes.CommitteeDutyTrace, error) {
					return &exportertypes.CommitteeDutyTrace{
						Slot: slot,
						ConsensusTrace: exportertypes.ConsensusTrace{
							Rounds: []*exportertypes.RoundTrace{
								{
									Proposer: spectypes.OperatorID(1),
									ProposalTrace: &exportertypes.ProposalTrace{
										QBFTTrace: exportertypes.QBFTTrace{
											Round:        1,
											BeaconRoot:   phase0.Root{1},
											Signer:       spectypes.OperatorID(1),
											ReceivedTime: uint64(time.Now().Unix()),
										},
									},
								},
							},
							Decideds: []*exportertypes.DecidedTrace{
								{
									Round:        1,
									BeaconRoot:   phase0.Root{1},
									Signers:      []spectypes.OperatorID{1, 2},
									ReceivedTime: uint64(time.Now().Unix()),
								},
							},
						},
					}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1)
				assert.Equal(t, phase0.Slot(100), resp.Data[0].Slot)
				assert.Equal(t, "ATTESTER", resp.Data[0].Role)
				assert.Equal(t, phase0.ValidatorIndex(1), resp.Data[0].Validator)
			},
		},
		{
			name: "valid request - attester role with no duty",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				// Mock GetCommitteeID to return a committee ID and validator index
				store.GetCommitteeIDFunc = func(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error) {
					return spectypes.CommitteeID{1}, 1, nil
				}

				// Mock GetCommitteeDuty to return ErrNotFound
				store.GetCommitteeDutyFunc = func(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exportertypes.CommitteeDutyTrace, error) {
					return nil, dutytracer.ErrNotFound
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // No duties should be returned when ErrNotFound
			},
		},
		{
			name: "error case - GetCommitteeDuty errors for attester role",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"ATTESTER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				// Mock GetCommitteeID to return a committee ID and validator index
				store.GetCommitteeIDFunc = func(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error) {
					return spectypes.CommitteeID{1}, 1, nil
				}

				// Mock GetCommitteeDuty to return ErrNotFound
				store.GetCommitteeDutyFunc = func(slot phase0.Slot, committeeID spectypes.CommitteeID) (*exportertypes.CommitteeDutyTrace, error) {
					return nil, fmt.Errorf("forced error on GetCommitteeDuty")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // No duties should be returned when ErrNotFound
			},
		},
		{
			name: "error case - GetCommitteeID returns error for sync committee",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"SYNC_COMMITTEE"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				// Mock GetCommitteeID to return error
				store.GetCommitteeIDFunc = func(slot phase0.Slot, pubkey spectypes.ValidatorPK) (spectypes.CommitteeID, phase0.ValidatorIndex, error) {
					return spectypes.CommitteeID{}, 0, fmt.Errorf("forced error on GetCommitteeID")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - GetValidatorDuty returns error for proposer",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				pkBytes := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
				var pk spectypes.ValidatorPK
				copy(pk[:], pkBytes)

				// Mock GetValidatorDuty to return error
				store.GetValidatorDutyFunc = func(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error) {
					return nil, fmt.Errorf("forced error on GetValidatorDuty")
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 0) // Should return empty array when error occurs
			},
		},
		{
			name: "error case - from > to",
			request: map[string]any{
				"from":    101,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock:      func(store *mockTraceStore, validatorStore *mockValidatorStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "'from' must be less than or equal to 'to'", resp.Message)
			},
		},
		{
			name: "error case - empty roles",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
			},
			setupMock:      func(store *mockTraceStore, validatorStore *mockValidatorStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Equal(t, "at least one role is required", resp.Message)
			},
		},
		{
			name: "invalid request - invalid pubkey length",
			request: map[string]any{
				"from":    100,
				"to":      200,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"0x1234"},
			},
			setupMock:      func(store *mockTraceStore, validatorStore *mockValidatorStore) {},
			expectedStatus: http.StatusBadRequest,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp struct {
					Status  string `json:"status"`
					Message string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				assert.Contains(t, resp.Message, "invalid pubkey length")
			},
		},
		{
			name: "valid request - both indices and pubkeys are provided for the same validator",
			request: map[string]any{
				"from":    100,
				"to":      100,
				"roles":   []string{"PROPOSER"},
				"pubkeys": []string{"b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"},
				"indices": []int{1},
			},
			setupMock: func(store *mockTraceStore, validatorStore *mockValidatorStore) {
				validatorStore.ValidatorByIndexFunc = func(index phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool) {
					pubkey := common.Hex2Bytes("b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1")
					var pk spectypes.ValidatorPK
					copy(pk[:], pubkey)
					return &ssvtypes.SSVShare{
						Share: spectypes.Share{
							ValidatorPubKey: pk,
						},
					}, true
				}
				store.GetValidatorDutyFunc = func(role spectypes.BeaconRole, slot phase0.Slot, pubkey spectypes.ValidatorPK) (*dutytracer.ValidatorDutyTrace, error) {
					return &dutytracer.ValidatorDutyTrace{}, nil
				}
			},
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp validatorTraceResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
				require.Len(t, resp.Data, 1) // deduplicated
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMockTraceStore()
			validatorStore := newMockValidatorStore()
			tt.setupMock(store, validatorStore)

			exporter := NewExporter(zap.NewNop(), nil, store, validatorStore)

			reqBody, err := json.Marshal(tt.request)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/traces/validator", strings.NewReader(string(reqBody)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			err = exporter.ValidatorTraces(rec, req)
			if err != nil {
				rec.Code = tt.expectedStatus
				errorResp := map[string]string{
					"status": http.StatusText(tt.expectedStatus),
					"error":  err.Error(),
				}
				jsonResp, _ := json.Marshal(errorResp)
				rec.Body.Write(jsonResp)
				rec.Header().Set("Content-Type", "application/json")
			}
			tt.validateResp(t, rec)
		})
	}
}

func TestExporterValidatorTraces_InvalidJSON(t *testing.T) {
	store := newMockTraceStore()
	validatorStore := newMockValidatorStore()
	exporter := NewExporter(zap.NewNop(), nil, store, validatorStore)

	req := httptest.NewRequest(http.MethodPost, "/traces/validator", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	err := exporter.ValidatorTraces(rec, req)
	if err != nil {
		rec.Code = http.StatusBadRequest
		errorResp := map[string]string{
			"status": http.StatusText(http.StatusBadRequest),
			"error":  err.Error(),
		}
		jsonResp, _ := json.Marshal(errorResp)
		rec.Body.Write(jsonResp)
		rec.Header().Set("Content-Type", "application/json")
	}

	var resp struct {
		Status  string `json:"status"`
		Message string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Message, "invalid character")
}

// mockValidatorStore is a mock implementation of storage.ValidatorStore
type mockValidatorStore struct {
	ValidatorByIndexFunc        func(phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool)
	CommitteeFunc               func(spectypes.CommitteeID) (*storage.Committee, bool)
	CommitteesFunc              func() []*storage.Committee
	OperatorCommitteesFunc      func(operatorID uint64) []*storage.Committee
	ValidatorIndexFunc          func(pubkey spectypes.ValidatorPK) (phase0.ValidatorIndex, bool)
	ValidatorFunc               func(pubKey []byte) (*ssvtypes.SSVShare, bool)
	ValidatorsFunc              func() []*ssvtypes.SSVShare
	ParticipatingValidatorsFunc func(epoch phase0.Epoch) []*ssvtypes.SSVShare
	OperatorValidatorsFunc      func(id spectypes.OperatorID) []*ssvtypes.SSVShare
	ParticipatingCommitteesFunc func(epoch phase0.Epoch) []*storage.Committee
	WithOperatorIDFunc          func(operatorID func() spectypes.OperatorID) storage.SelfValidatorStore
}

func newMockValidatorStore() *mockValidatorStore {
	return &mockValidatorStore{}
}

func (m *mockValidatorStore) ValidatorByIndex(index phase0.ValidatorIndex) (*ssvtypes.SSVShare, bool) {
	if m.ValidatorByIndexFunc != nil {
		return m.ValidatorByIndexFunc(index)
	}
	return nil, false
}

func (m *mockValidatorStore) Committee(id spectypes.CommitteeID) (*storage.Committee, bool) {
	if m.CommitteeFunc != nil {
		return m.CommitteeFunc(id)
	}
	return nil, false
}

func (m *mockValidatorStore) Committees() []*storage.Committee {
	if m.CommitteesFunc != nil {
		return m.CommitteesFunc()
	}
	return nil
}

func (m *mockValidatorStore) OperatorCommittees(operatorID uint64) []*storage.Committee {
	if m.OperatorCommitteesFunc != nil {
		return m.OperatorCommitteesFunc(operatorID)
	}
	return nil
}

func (m *mockValidatorStore) ValidatorIndex(pubkey spectypes.ValidatorPK) (phase0.ValidatorIndex, bool) {
	if m.ValidatorIndexFunc != nil {
		return m.ValidatorIndexFunc(pubkey)
	}
	return 0, false
}

func (m *mockValidatorStore) Validator(pubKey []byte) (*ssvtypes.SSVShare, bool) {
	if m.ValidatorFunc != nil {
		return m.ValidatorFunc(pubKey)
	}
	return nil, false
}

func (m *mockValidatorStore) Validators() []*ssvtypes.SSVShare {
	if m.ValidatorsFunc != nil {
		return m.ValidatorsFunc()
	}
	return nil
}

func (m *mockValidatorStore) ParticipatingValidators(epoch phase0.Epoch) []*ssvtypes.SSVShare {
	if m.ParticipatingValidatorsFunc != nil {
		return m.ParticipatingValidatorsFunc(epoch)
	}
	return nil
}

func (m *mockValidatorStore) OperatorValidators(id spectypes.OperatorID) []*ssvtypes.SSVShare {
	if m.OperatorValidatorsFunc != nil {
		return m.OperatorValidatorsFunc(id)
	}
	return nil
}

func (m *mockValidatorStore) ParticipatingCommittees(epoch phase0.Epoch) []*storage.Committee {
	if m.ParticipatingCommitteesFunc != nil {
		return m.ParticipatingCommitteesFunc(epoch)
	}
	return nil
}

func (m *mockValidatorStore) WithOperatorID(operatorID func() spectypes.OperatorID) storage.SelfValidatorStore {
	if m.WithOperatorIDFunc != nil {
		return m.WithOperatorIDFunc(operatorID)
	}
	return nil
}
