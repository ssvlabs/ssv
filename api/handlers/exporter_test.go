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

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/operator/slotticker"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
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

func (m *mockParticipantStore) Prune(context.Context, *zap.Logger, phase0.Slot) {
	// no-op.
}

func (m *mockParticipantStore) PruneContinously(context.Context, *zap.Logger, slotticker.Provider, phase0.Slot) {
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

			exporter := &Exporter{
				ParticipantStores: stores,
			}

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

	exporter := &Exporter{
		ParticipantStores: stores,
	}
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

	exporter := &Exporter{
		ParticipantStores: stores,
	}
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

	exporter := &Exporter{
		ParticipantStores: stores,
	}

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
