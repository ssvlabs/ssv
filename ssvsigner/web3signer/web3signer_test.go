package web3signer

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Web3Signer) {
	server := httptest.NewServer(handler)

	t.Cleanup(func() {
		server.Close()
	})

	web3Signer := New(server.URL)

	return server, web3Signer
}

func TestNew(t *testing.T) {
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
			client := New(tt.baseURL)
			require.NotNil(t, client)

			expectedBaseURL := tt.baseURL
			if expectedBaseURL[len(expectedBaseURL)-1] == '/' {
				expectedBaseURL = expectedBaseURL[:len(expectedBaseURL)-1]
			}

			require.Equal(t, expectedBaseURL, client.baseURL)
		})
	}
}

func TestListKeys(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		resp        ListKeysResponse
		errContains string
	}{
		{
			name:       "Successful list keys",
			statusCode: http.StatusOK,
			resp: ListKeysResponse{
				mustBLSFromString("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
				mustBLSFromString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
			},
		},
		{
			name:       "Empty list",
			statusCode: http.StatusOK,
			resp:       ListKeysResponse{},
		},
		{
			name:        "Server error",
			statusCode:  http.StatusInternalServerError,
			resp:        ListKeysResponse{},
			errContains: "unexpected status: 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, pathPublicKeys, r.URL.Path)
				require.Equal(t, http.MethodGet, r.Method)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.resp))
			})

			keys, err := web3Signer.ListKeys(context.Background())

			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.resp, keys)
			}
		})
	}
}

func TestImportKeystore(t *testing.T) {
	tests := []struct {
		name        string
		req         ImportKeystoreRequest
		statusCode  int
		response    ImportKeystoreResponse
		containsErr string
	}{
		{
			name: "Successful import",
			req: ImportKeystoreRequest{
				Keystores: []string{
					`{"crypto":{"kdf":{"function":"scrypt","params":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"message":""},"checksum":{"function":"sha256","params":{},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"cipher":{"function":"aes-128-ctr","params":{"iv":"0123456789abcdef0123456789abcdef"},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},"description":"","pubkey":"0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","path":"","uuid":"00000000-0000-0000-0000-000000000000","version":4}`,
				},
				Passwords: []string{"password123"},
			},
			statusCode: http.StatusOK,
			response: ImportKeystoreResponse{
				Data: []KeyManagerResponseData{
					{
						Status:  "imported",
						Message: "Key successfully imported",
					},
				},
			},
		},
		{
			name: "Failed import",
			req: ImportKeystoreRequest{
				Keystores: []string{
					`{"crypto":{"kdf":{"function":"scrypt","params":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"message":""},"checksum":{"function":"sha256","params":{},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"cipher":{"function":"aes-128-ctr","params":{"iv":"0123456789abcdef0123456789abcdef"},"message":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}},"description":"","pubkey":"0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","path":"","uuid":"00000000-0000-0000-0000-000000000000","version":4}`,
				},
				Passwords: []string{"wrongpassword"},
			},
			statusCode: http.StatusBadRequest,
			response: ImportKeystoreResponse{
				Message: "failed to import keystore",
			},
			containsErr: "error status 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, pathKeystores, r.URL.Path)
				require.Equal(t, http.MethodPost, r.Method)

				var req ImportKeystoreRequest

				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

				if !reflect.DeepEqual(req, tt.req) {
					t.Errorf("Expected req %v but got %v", tt.req, req)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.response))
			})

			data, err := web3Signer.ImportKeystore(context.Background(), tt.req)

			if tt.containsErr != "" {
				require.ErrorContains(t, err, tt.containsErr)
			} else {
				require.NoError(t, err)

				if !reflect.DeepEqual(data, tt.response) {
					t.Errorf("Expected resp %v but got %v", tt.response, data)
				}
			}
		})
	}
}

func TestDeleteKeystore(t *testing.T) {
	tests := []struct {
		name                 string
		req                  DeleteKeystoreRequest
		statusCode           int
		response             DeleteKeystoreResponse
		expectedResponseData []KeyManagerResponseData
		containsErr          string
	}{
		{
			name: "Successful delete",
			req: DeleteKeystoreRequest{
				Pubkeys: []phase0.BLSPubKey{
					mustBLSFromString("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
				},
			},
			statusCode: http.StatusOK,
			response: DeleteKeystoreResponse{
				Data: []KeyManagerResponseData{
					{
						Status:  "deleted",
						Message: "Key successfully deleted",
					},
				},
			},
		},
		{
			name: "Failed delete",
			req: DeleteKeystoreRequest{
				Pubkeys: []phase0.BLSPubKey{
					mustBLSFromString("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
				},
			},
			statusCode: http.StatusBadRequest,
			response: DeleteKeystoreResponse{
				Message: "failed to delete keystore",
			},
			containsErr: "unexpected status: 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, pathKeystores, r.URL.Path)
				require.Equal(t, http.MethodDelete, r.Method)

				var req DeleteKeystoreRequest

				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

				if !reflect.DeepEqual(req, tt.req) {
					t.Errorf("Expected req %v but got %v", tt.req, req.Pubkeys)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.response))
			})

			resp, err := web3Signer.DeleteKeystore(context.Background(), tt.req)

			if tt.containsErr != "" {
				require.ErrorContains(t, err, tt.containsErr)
			} else {
				require.NoError(t, err)

				if !reflect.DeepEqual(resp, tt.response) {
					t.Errorf("Expected resp %v but got %v", tt.response, resp)
				}
			}
		})
	}
}

func TestSign(t *testing.T) {
	testPayload := SignRequest{
		Type: TypeAggregationSlot,
		ForkInfo: ForkInfo{
			Fork: &phase0.Fork{
				PreviousVersion: [4]byte{0, 0, 0, 0},
				CurrentVersion:  [4]byte{1, 0, 0, 0},
				Epoch:           0,
			},
			GenesisValidatorsRoot: phase0.Root{},
		},
		AggregationSlot: &AggregationSlot{
			Slot: 1,
		},
	}

	tests := []struct {
		name           string
		sharePubKey    phase0.BLSPubKey
		payload        SignRequest
		statusCode     int
		response       SignResponse
		expectedResult []byte
		expectError    bool
	}{
		{
			name:           "Successful sign",
			sharePubKey:    phase0.BLSPubKey{0x01, 0x02, 0x03},
			payload:        testPayload,
			statusCode:     http.StatusOK,
			response:       SignResponse{Signature: phase0.BLSSignature(bytes.Repeat([]byte{1}, phase0.SignatureLength))},
			expectedResult: bytes.Repeat([]byte{1}, phase0.SignatureLength),
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, pathSign+tt.sharePubKey.String(), r.URL.Path)
				require.Equal(t, http.MethodPost, r.Method)

				var req SignRequest

				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				require.Equal(t, tt.payload, req)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				require.NoError(t, json.NewEncoder(w).Encode(tt.response))
			})

			resp, err := web3Signer.Sign(context.Background(), tt.sharePubKey, tt.payload)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tt.expectedResult, resp.Signature)
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
