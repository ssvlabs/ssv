package web3signer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/tls/testingutils"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Web3Signer) {
	t.Helper()

	server := httptest.NewServer(handler)

	t.Cleanup(func() {
		server.Close()
	})

	web3Signer, err := New(server.URL)
	require.NoError(t, err)

	return server, web3Signer
}

func mustWriteTemp(t *testing.T, data []byte, pattern string) string {
	t.Helper()

	f, err := os.CreateTemp("", pattern)
	require.NoError(t, err)

	filename := f.Name()

	// clean up the file after a test
	t.Cleanup(func() {
		os.Remove(filename)
	})

	_, err = f.Write(data)
	require.NoError(t, err)

	err = f.Close()
	require.NoError(t, err)

	return filename
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
		certPath        string
		keyPath         string
		caPath          string
		expectedBaseURL string
	}{
		{"ValidURL", "http://localhost:9000", "", "", "", "http://localhost:9000"},
		{"WithSlash", "http://localhost:9000/", "", "", "", "http://localhost:9000"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.baseURL, WithTLS(tt.certPath, tt.keyPath, tt.caPath))
			require.NoError(t, err)
			require.Equal(t, tt.expectedBaseURL, client.baseURL)
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

func TestTLSConfigFiles(t *testing.T) {
	t.Parallel()

	ca, _, cert, key := testingutils.GenerateCertificates(t, "localhost")
	certFile := mustWriteTemp(t, cert, "cert-*.pem")
	keyFile := mustWriteTemp(t, key, "key-*.pem")
	caFile := mustWriteTemp(t, ca, "ca-*.pem")

	client, err := New("https://example.com", WithTLS(certFile, keyFile, caFile))
	require.NoError(t, err)

	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok)

	cfg := transport.TLSClientConfig
	require.NotNil(t, cfg)
	require.Len(t, cfg.Certificates, 1)
	require.NotNil(t, cfg.RootCAs)
}

func TestTLSConnection(t *testing.T) {
	t.Parallel()

	caCert, _, serverCert, serverKey := testingutils.GenerateCertificates(t, "localhost")
	serverTLSCert, err := tls.X509KeyPair(serverCert, serverKey)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	})

	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverTLSCert}}

	srv.StartTLS()
	defer srv.Close()

	// success with CA
	caFile := mustWriteTemp(t, caCert, "ca-*.pem")
	client, err := New(srv.URL, WithTLS("", "", caFile))
	require.NoError(t, err)

	_, err = client.ListKeys(context.Background())
	require.NoError(t, err)

	// failure without CA
	client2, err := New(srv.URL)
	require.NoError(t, err)

	_, err = client2.ListKeys(context.Background())
	require.Error(t, err)
	require.ErrorAs(t, err, &HTTPResponseError{})
	require.Contains(t, err.Error(), "failed to verify certificate")
}
