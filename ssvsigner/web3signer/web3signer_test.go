package web3signer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Web3Signer) {
	t.Helper()

	server := httptest.NewServer(handler)

	t.Cleanup(func() {
		server.Close()
	})

	web3Signer := New(server.URL)
	require.NotNil(t, web3Signer)

	return server, web3Signer
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

func TestNew(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		baseURL         string
		expectedBaseURL string
	}{
		{
			name:    "Valid URL",
			baseURL: "http://localhost:9000",
		},
		{
			name:    "Valid URL with trailing slash",
			baseURL: "http://localhost:9000/",
		},
		{
			name:    "Empty URL",
			baseURL: "",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := New(tt.baseURL)
			require.NotNil(t, client)

			expectedBaseURL := tt.baseURL
			if len(expectedBaseURL) != 0 && expectedBaseURL[len(expectedBaseURL)-1] == '/' {
				expectedBaseURL = expectedBaseURL[:len(expectedBaseURL)-1]
			}

			require.Equal(t, expectedBaseURL, client.baseURL)
		})
	}
}

func TestListKeys(t *testing.T) {
	t.Parallel()

	testCases := []struct {
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

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, pathPublicKeys, r.URL.Path)

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
	t.Parallel()

	testCases := []struct {
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

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, pathKeystores, r.URL.Path)

				var req ImportKeystoreRequest

				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				require.Equal(t, tt.req, req)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)

				require.NoError(t, json.NewEncoder(w).Encode(tt.response))
			})

			data, err := web3Signer.ImportKeystore(context.Background(), tt.req)

			if tt.containsErr != "" {
				require.ErrorContains(t, err, tt.containsErr)
				require.Empty(t, data)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.response, data)
			}
		})
	}
}

func TestDeleteKeystore(t *testing.T) {
	t.Parallel()

	testCases := []struct {
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

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodDelete, r.Method)
				require.Equal(t, pathKeystores, r.URL.Path)

				var req DeleteKeystoreRequest

				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				require.Equal(t, tt.req, req)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)

				require.NoError(t, json.NewEncoder(w).Encode(tt.response))
			})

			resp, err := web3Signer.DeleteKeystore(context.Background(), tt.req)

			if tt.containsErr != "" {
				require.ErrorContains(t, err, tt.containsErr)
				require.Empty(t, resp)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.response, resp)
			}
		})
	}
}

func TestSign(t *testing.T) {
	testPubKey := mustBLSFromString("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
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

	testCases := []struct {
		name            string
		pubKey          phase0.BLSPubKey
		payload         SignRequest
		setupServer     func(w http.ResponseWriter, r *http.Request)
		wantSignature   []byte
		wantErrContains string
	}{
		{
			name:    "successful signing",
			pubKey:  testPubKey,
			payload: testPayload,
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)

				var req SignRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				require.Equal(t, testPayload, req)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				response := SignResponse{Signature: phase0.BLSSignature(bytes.Repeat([]byte{1}, phase0.SignatureLength))}

				require.NoError(t, json.NewEncoder(w).Encode(response))
			},
			wantSignature: bytes.Repeat([]byte{1}, phase0.SignatureLength),
		},
		{
			name:    "server error",
			pubKey:  testPubKey,
			payload: testPayload,
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)

				require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
					"error": "internal server error",
				}))
			},
			wantErrContains: "error status 500",
		},
		{
			name:    "bad request error",
			pubKey:  testPubKey,
			payload: testPayload,
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)

				require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
					"error": "invalid request",
				}))
			},
			wantErrContains: "error status 400",
		},
		{
			name:    "network error",
			pubKey:  testPubKey,
			payload: testPayload,
			setupServer: func(w http.ResponseWriter, r *http.Request) {
				// close connection without response to simulate network error
				hj, ok := w.(http.Hijacker)
				require.True(t, ok)

				conn, _, err := hj.Hijack()
				require.NoError(t, err)

				conn.Close()
			},
			wantErrContains: "error status 500",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, web3Signer := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				expectedPath := pathSign + tt.pubKey.String()

				require.Equal(t, expectedPath, r.URL.Path)

				tt.setupServer(w, r)
			})

			resp, err := web3Signer.Sign(context.Background(), tt.pubKey, tt.payload)

			if tt.wantErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrContains)

				var httpErr HTTPResponseError
				require.ErrorAs(t, err, &httpErr)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, tt.wantSignature, resp.Signature)
			}
		})
	}
}

func TestTLSConfig(t *testing.T) {
	t.Parallel()

	cert := []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`)

	key := []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`)

	tlsCert, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}

	client := New("https://example.com", WithTLS(tlsConfig))

	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok)

	cfg := transport.TLSClientConfig
	require.NotNil(t, cfg)
	require.Len(t, cfg.Certificates, 1)
}
