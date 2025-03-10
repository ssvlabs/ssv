package web3signer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Web3Signer) {
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
	})

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	web3Signer := New(logger, server.URL)
	return server, web3Signer
}

func TestNew(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	tests := []struct {
		name    string
		baseURL string
	}{
		{
			name:    "Valid URL",
			baseURL: "http://localhost:9000",
		},
		{
			name:    "Valid URL with trailing slash",
			baseURL: "http://localhost:9000/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := New(logger, tt.baseURL)
			require.NotNil(t, client)

			expectedBaseURL := tt.baseURL
			if expectedBaseURL[len(expectedBaseURL)-1] == '/' {
				expectedBaseURL = expectedBaseURL[:len(expectedBaseURL)-1]
			}

			require.Equal(t, expectedBaseURL, client.baseURL)
		})
	}
}

func TestImportKeystore(t *testing.T) {
	tests := []struct {
		name                 string
		keystoreList         []string
		keystorePasswordList []string
		statusCode           int
		responseBody         ImportKeystoreResponse
		expectedStatuses     []Status
		expectError          bool
	}{
		{
			name: "Successful import",
			keystoreList: []string{
				`{"crypto":{"kdf":{"function":"scrypt","params":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"message":""},"checksum":{"function":"sha256","params":{},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"cipher":{"function":"aes-128-ctr","params":{"iv":"0123456789abcdef0123456789abcdef"},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},"description":"","pubkey":"0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","path":"","uuid":"00000000-0000-0000-0000-000000000000","version":4}`,
			},
			keystorePasswordList: []string{"password123"},
			statusCode:           http.StatusOK,
			responseBody: ImportKeystoreResponse{
				Data: []KeyManagerResponseData{
					{
						Status:  "imported",
						Message: "Key successfully imported",
					},
				},
			},
			expectedStatuses: []Status{"imported"},
			expectError:      false,
		},
		{
			name: "Failed import",
			keystoreList: []string{
				`{"crypto":{"kdf":{"function":"scrypt","params":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"message":""},"checksum":{"function":"sha256","params":{},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"cipher":{"function":"aes-128-ctr","params":{"iv":"0123456789abcdef0123456789abcdef"},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},"description":"","pubkey":"0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","path":"","uuid":"00000000-0000-0000-0000-000000000000","version":4}`,
			},
			keystorePasswordList: []string{"wrongpassword"},
			statusCode:           http.StatusBadRequest,
			responseBody: ImportKeystoreResponse{
				Message: "Failed to import keystore",
				Data: []KeyManagerResponseData{
					{
						Status:  "error",
						Message: "Invalid password",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/eth/v1/keystores", r.URL.Path)
				require.Equal(t, http.MethodPost, r.Method)

				var req ImportKeystoreRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

				if !reflect.DeepEqual(req.Keystores, tt.keystoreList) {
					t.Errorf("Expected keystores %v but got %v", tt.keystoreList, req.Keystores)
				}
				if !reflect.DeepEqual(req.Passwords, tt.keystorePasswordList) {
					t.Errorf("Expected passwords %v but got %v", tt.keystorePasswordList, req.Passwords)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.responseBody))
			})

			statuses, err := web3Signer.ImportKeystore(context.Background(), tt.keystoreList, tt.keystorePasswordList)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if !reflect.DeepEqual(statuses, tt.expectedStatuses) {
					t.Errorf("Expected statuses %v but got %v", tt.expectedStatuses, statuses)
				}
			}
		})
	}
}

func TestDeleteKeystore(t *testing.T) {
	tests := []struct {
		name             string
		sharePubKeyList  []phase0.BLSPubKey
		statusCode       int
		responseBody     DeleteKeystoreResponse
		expectedStatuses []Status
		expectError      bool
	}{
		{
			name: "Successful delete",
			sharePubKeyList: []phase0.BLSPubKey{
				mustBLSFromString("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
			},
			statusCode: http.StatusOK,
			responseBody: DeleteKeystoreResponse{
				Data: []KeyManagerResponseData{
					{
						Status:  "deleted",
						Message: "Key successfully deleted",
					},
				},
			},
			expectedStatuses: []Status{"deleted"},
			expectError:      false,
		},
		{
			name: "Failed delete",
			sharePubKeyList: []phase0.BLSPubKey{
				mustBLSFromString("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
			},
			statusCode: http.StatusBadRequest,
			responseBody: DeleteKeystoreResponse{
				Message: "Failed to delete keystore",
				Data: []KeyManagerResponseData{
					{
						Status:  "error",
						Message: "Key not found",
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/eth/v1/keystores", r.URL.Path)
				require.Equal(t, http.MethodDelete, r.Method)

				var req DeleteKeystoreRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

				if !reflect.DeepEqual(req.Pubkeys, tt.sharePubKeyList) {
					t.Errorf("Expected pubkeys %v but got %v", tt.sharePubKeyList, req.Pubkeys)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.responseBody))
			})

			statuses, err := web3Signer.DeleteKeystore(context.Background(), tt.sharePubKeyList)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if !reflect.DeepEqual(statuses, tt.expectedStatuses) {
					t.Errorf("Expected statuses %v but got %v", tt.expectedStatuses, statuses)
				}
			}
		})
	}
}

func TestSign(t *testing.T) {
	testPayload := SignRequest{
		Type: AggregationSlot,
		ForkInfo: ForkInfo{
			Fork: &phase0.Fork{
				PreviousVersion: [4]byte{0, 0, 0, 0},
				CurrentVersion:  [4]byte{1, 0, 0, 0},
				Epoch:           0,
			},
			GenesisValidatorsRoot: phase0.Root{},
		},
		AggregationSlot: &AggregationSlotData{
			Slot: 1,
		},
	}

	tests := []struct {
		name           string
		sharePubKey    phase0.BLSPubKey
		payload        SignRequest
		statusCode     int
		responseBody   string
		expectedResult []byte
		expectError    bool
	}{
		{
			name:        "Successful sign",
			sharePubKey: phase0.BLSPubKey{0x01, 0x02, 0x03},
			payload:     testPayload,
			statusCode:  http.StatusOK,
			responseBody: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef" +
				"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			expectedResult: []byte{
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
				0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef,
			},
			expectError: false,
		},
		{
			name:         "Invalid public key",
			sharePubKey:  phase0.BLSPubKey{0x01, 0x02, 0x03},
			payload:      testPayload,
			statusCode:   http.StatusBadRequest,
			responseBody: `{"message": "Invalid public key"}`,
			expectError:  true,
		},
		{
			name:         "Server error",
			sharePubKey:  phase0.BLSPubKey{0x01, 0x02, 0x03},
			payload:      testPayload,
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"message": "Internal server error"}`,
			expectError:  true,
		},
		{
			name:         "Invalid response format",
			sharePubKey:  phase0.BLSPubKey{0x01, 0x02, 0x03},
			payload:      testPayload,
			statusCode:   http.StatusOK,
			responseBody: "invalid-hex",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				expectedPath := fmt.Sprintf("/api/v1/eth2/sign/%s", tt.sharePubKey.String())
				require.Equal(t, expectedPath, r.URL.Path)
				require.Equal(t, http.MethodPost, r.Method)

				var req SignRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				require.Equal(t, tt.payload, req)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, err := w.Write([]byte(tt.responseBody))
				require.NoError(t, err)
			})

			result, err := web3Signer.Sign(context.Background(), tt.sharePubKey, tt.payload)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestListKeys(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody ListKeysResponse
		expectedKeys []phase0.BLSPubKey
		expectError  bool
	}{
		{
			name:       "Successful list keys",
			statusCode: http.StatusOK,
			responseBody: ListKeysResponse{
				mustBLSFromString("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
				mustBLSFromString("0x123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
			},
			expectedKeys: []phase0.BLSPubKey{
				mustBLSFromString("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
				mustBLSFromString("0x123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
			},
			expectError: false,
		},
		{
			name:         "Empty list",
			statusCode:   http.StatusOK,
			responseBody: ListKeysResponse{},
			expectedKeys: []phase0.BLSPubKey{},
			expectError:  false,
		},
		{
			name:         "Server error",
			statusCode:   http.StatusInternalServerError,
			responseBody: ListKeysResponse{},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/v1/eth2/publicKeys", r.URL.Path)
				require.Equal(t, http.MethodGet, r.Method)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.responseBody))
			})

			keys, err := web3Signer.ListKeys(context.Background())

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedKeys, keys)
			}
		})
	}
}

func mustBLSFromString(s string) phase0.BLSPubKey {
	pk, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}

	if len(pk) != len(phase0.BLSPubKey{}) {
		panic("invalid public key length")
	}

	return phase0.BLSPubKey(pk)
}
