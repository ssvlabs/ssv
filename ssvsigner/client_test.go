package ssvsigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"
	"github.com/ssvlabs/ssv/ssvsigner/tls/testingutils"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type SSVSignerClientSuite struct {
	suite.Suite
	server     *httptest.Server
	client     *Client
	logger     *zap.Logger
	mux        *http.ServeMux
	serverHits int
}

func (s *SSVSignerClientSuite) SetupTest() {
	var err error
	s.logger, err = zap.NewDevelopment()
	s.Require().NoError(err, "Failed to create logger")

	s.mux = http.NewServeMux()
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serverHits++
		s.mux.ServeHTTP(w, r)
	}))
	s.client, err = NewClient(s.server.URL, WithLogger(s.logger))
	s.Require().NoError(err, "failed to create client")
}

func (s *SSVSignerClientSuite) TearDownTest() {
	s.server.Close()
	s.serverHits = 0
}

// resetMux resets the mux handler between test cases.
func (s *SSVSignerClientSuite) resetMux() {
	s.mux = http.NewServeMux()
	s.serverHits = 0
}

// assertErrorResult asserts that the error matches expectations.
func (s *SSVSignerClientSuite) assertErrorResult(err error, expectError bool, t *testing.T) {
	if expectError {
		require.Error(t, err, "Expected an error")
	} else {
		require.NoError(t, err, "Unexpected error")
	}
	assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
}

// writeJSONResponse writes a JSON response with the given status code and data.
func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.WriteHeader(statusCode)

	if statusCode == http.StatusOK && data != nil {
		respBytes, err := json.Marshal(data)
		if err == nil {
			w.Write(respBytes)
			return
		}
	}

	// For non-OK status codes or marshal errors
	if statusCode == http.StatusUnprocessableEntity {
		w.Write([]byte("Decryption error"))
	} else if statusCode != http.StatusOK {
		w.Write([]byte("Server error"))
	}
}

func (s *SSVSignerClientSuite) TestAddValidators() {
	t := s.T()

	testCases := []struct {
		name               string
		shares             []ShareKeys
		expectedStatusCode int
		expectedResponse   web3signer.ImportKeystoreResponse
		expectError        bool
		isDecryptionError  bool
	}{
		{
			name: "Success", // TODO: fix
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted1"),
					PubKey:           phase0.BLSPubKey{1, 2, 3},
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedResponse: web3signer.ImportKeystoreResponse{
				Data: []web3signer.KeyManagerResponseData{
					{
						Status:  web3signer.StatusImported,
						Message: "imported",
					},
				},
			},
			expectError: false,
		},
		{
			name: "DecryptionError", // TODO: fix
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("bad_encrypted"),
					PubKey:           phase0.BLSPubKey{1, 2, 3},
				},
			},
			expectedStatusCode: http.StatusUnprocessableEntity,
			expectedResponse:   web3signer.ImportKeystoreResponse{},
			expectError:        true,
			isDecryptionError:  true,
		},
		{
			name: "ServerError", // TODO: fix
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted"),
					PubKey:           phase0.BLSPubKey{1, 2, 3},
				},
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   web3signer.ImportKeystoreResponse{},
			expectError:        true,
		},
		{
			name:               "NoShares",
			shares:             []ShareKeys{},
			expectedStatusCode: http.StatusOK,
			expectedResponse: web3signer.ImportKeystoreResponse{
				Data: []web3signer.KeyManagerResponseData{},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()
			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req AddValidatorRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				require.Len(t, req.ShareKeys, len(tc.shares))

				for i, share := range tc.shares {
					assert.Equal(t, share.EncryptedPrivKey, req.ShareKeys[i].EncryptedPrivKey)
					assert.EqualValues(t, share.PubKey[:], req.ShareKeys[i].PubKey)
				}

				writeJSONResponse(w, tc.expectedStatusCode, tc.expectedResponse)
			})

			err := s.client.AddValidators(context.Background(), tc.shares...)

			s.assertErrorResult(err, tc.expectError, t)

			if tc.isDecryptionError {
				var decryptErr ShareDecryptionError
				assert.ErrorAs(t, err, &decryptErr, "Expected a ShareDecryptionError")
			}
		})
	}
}

func (s *SSVSignerClientSuite) TestRemoveValidators() {
	t := s.T()

	testCases := []struct {
		name               string
		pubKeys            []phase0.BLSPubKey
		expectedStatusCode int
		expectedResponse   web3signer.DeleteKeystoreResponse
		expectError        bool
	}{
		{
			name: "Success",
			pubKeys: []phase0.BLSPubKey{
				{1, 2, 3},
			},
			expectedStatusCode: http.StatusOK,
			expectedResponse: web3signer.DeleteKeystoreResponse{
				Data: []web3signer.KeyManagerResponseData{
					{
						Status:  web3signer.StatusDeleted,
						Message: "deleted",
					},
				},
			},
			expectError: false,
		},
		{
			name: "ServerError",
			pubKeys: []phase0.BLSPubKey{
				{1, 2, 3},
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   web3signer.DeleteKeystoreResponse{},
			expectError:        true,
		},
		{
			name:               "NoPubKeys",
			pubKeys:            []phase0.BLSPubKey{},
			expectedStatusCode: http.StatusOK,
			expectedResponse: web3signer.DeleteKeystoreResponse{
				Data: []web3signer.KeyManagerResponseData{},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()
			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodDelete, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req web3signer.DeleteKeystoreRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				require.Len(t, req.Pubkeys, len(tc.pubKeys))

				for i, pubKey := range tc.pubKeys {
					assert.Equal(t, pubKey, req.Pubkeys[i])
				}

				writeJSONResponse(w, tc.expectedStatusCode, tc.expectedResponse)
			})

			err := s.client.RemoveValidators(context.Background(), tc.pubKeys...)

			s.assertErrorResult(err, tc.expectError, t)
		})
	}
}

func (s *SSVSignerClientSuite) TestListValidators() {
	t := s.T()

	testCases := []struct {
		name               string
		expectedStatusCode int
		expectedResponse   interface{}
		expectedResult     []phase0.BLSPubKey
		expectError        bool
	}{
		{
			name:               "Success", // TODO: fix
			expectedStatusCode: http.StatusOK,
			expectedResponse: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			expectedResult: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			expectError: false,
		},
		{
			name:               "EmptyList", // TODO: fix
			expectedStatusCode: http.StatusOK,
			expectedResponse:   web3signer.ListKeysResponse{},
			expectedResult:     web3signer.ListKeysResponse{},
			expectError:        false,
		},
		{
			name:               "ServerError",
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   nil,
			expectedResult:     nil,
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()
			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				writeJSONResponse(w, tc.expectedStatusCode, tc.expectedResponse)
			})

			result, err := s.client.ListValidators(context.Background())

			if tc.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}
			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
	}
}

func (s *SSVSignerClientSuite) TestSign() {
	t := s.T()

	samplePubKey := phase0.BLSPubKey{1, 1, 1, 1}
	samplePayload := web3signer.SignRequest{
		Type: web3signer.TypeAttestation,
		ForkInfo: web3signer.ForkInfo{
			Fork: &phase0.Fork{
				PreviousVersion: phase0.Version{0, 0, 0, 0},
				CurrentVersion:  phase0.Version{1, 0, 0, 0},
				Epoch:           0,
			},
			GenesisValidatorsRoot: phase0.Root{},
		},
		Attestation: &phase0.AttestationData{
			Slot:            1,
			Index:           2,
			BeaconBlockRoot: phase0.Root{},
			Source: &phase0.Checkpoint{
				Epoch: 0,
				Root:  phase0.Root{},
			},
			Target: &phase0.Checkpoint{
				Epoch: 1,
				Root:  phase0.Root{},
			},
		},
	}

	testCases := []struct {
		name               string
		pubKey             phase0.BLSPubKey
		payload            web3signer.SignRequest
		expectedStatusCode int
		responseBody       string
		expectedResult     phase0.BLSSignature
		expectError        bool
	}{
		{
			name:               "Success", // TODO: fix
			pubKey:             samplePubKey,
			payload:            samplePayload,
			expectedStatusCode: http.StatusOK,
			responseBody:       fmt.Sprintf(`{"signature":"%s"}`, "0x"+hex.EncodeToString(bytes.Repeat([]byte{1}, phase0.SignatureLength))),
			expectedResult:     phase0.BLSSignature(bytes.Repeat([]byte{1}, phase0.SignatureLength)),
			expectError:        false,
		},
		{
			name:               "InvalidSignature",
			pubKey:             samplePubKey,
			payload:            samplePayload,
			expectedStatusCode: http.StatusBadRequest,
			responseBody:       "invalid signature",
			expectError:        true,
		},
		{
			name:               "ServerError",
			pubKey:             samplePubKey,
			payload:            samplePayload,
			expectedStatusCode: http.StatusInternalServerError,
			responseBody:       "Server error",
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()
			s.mux.HandleFunc(pathValidatorsSign+tc.pubKey.String(), func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req web3signer.SignRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				w.WriteHeader(tc.expectedStatusCode)
				w.Write([]byte(tc.responseBody))
			})

			result, err := s.client.Sign(context.Background(), tc.pubKey, tc.payload)

			if tc.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}
			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
	}
}

func (s *SSVSignerClientSuite) TestOperatorIdentity() {
	t := s.T()

	expectedIdentity := "operator_identity_key"

	testCases := []struct {
		name               string
		expectedStatusCode int
		expectedResult     string
		expectError        bool
	}{
		{
			name:               "Success",
			expectedStatusCode: http.StatusOK,
			expectedResult:     expectedIdentity,
			expectError:        false,
		},
		{
			name:               "ServerError",
			expectedStatusCode: http.StatusInternalServerError,
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()
			s.mux.HandleFunc(pathOperatorIdentity, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					w.Write([]byte(tc.expectedResult))
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.OperatorIdentity(context.Background())

			if tc.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}
			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
	}
}

func (s *SSVSignerClientSuite) TestOperatorSign() {
	t := s.T()

	samplePayload := []byte(`{"data":"to_sign"}`)
	expectedSignature := []byte("signed_data")

	testCases := []struct {
		name               string
		payload            []byte
		expectedStatusCode int
		expectedResult     []byte
		expectError        bool
	}{
		{
			name:               "Success", // TODO: fix
			payload:            samplePayload,
			expectedStatusCode: http.StatusOK,
			expectedResult:     expectedSignature,
			expectError:        false,
		},
		{
			name:               "ServerError", // TODO: fix
			payload:            samplePayload,
			expectedStatusCode: http.StatusInternalServerError,
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()
			s.mux.HandleFunc(pathOperatorSign, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				assert.Equal(t, tc.payload, body)

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					w.Write(tc.expectedResult)
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.OperatorSign(context.Background(), tc.payload)

			if tc.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}
			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
	}
}

// TestMissingKeys tests the MissingKeys method which identifies keys present in local storage
// but missing from the remote SSV signer service. It verifies proper key difference calculation
// and error handling for various key combinations and server response scenarios.
func (s *SSVSignerClientSuite) TestMissingKeys() {
	t := s.T()

	testCases := []struct {
		name         string
		localKeys    []phase0.BLSPubKey
		remoteKeys   []phase0.BLSPubKey
		expectedKeys []phase0.BLSPubKey
		listError    bool
		expectError  bool
	}{
		{
			name: "NoMissingKeys",
			localKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			remoteKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
				{7, 8, 9},
			},
			expectedKeys: nil,
			listError:    false,
			expectError:  false,
		},
		{
			name: "SomeMissingKeys",
			localKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
				{10, 11, 12},
			},
			remoteKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{7, 8, 9},
			},
			expectedKeys: []phase0.BLSPubKey{
				{4, 5, 6},
				{10, 11, 12},
			},
			listError:   false,
			expectError: false,
		},
		{
			name: "AllMissingKeys",
			localKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			remoteKeys: []phase0.BLSPubKey{
				{7, 8, 9},
				{10, 11, 12},
			},
			expectedKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			listError:   false,
			expectError: false,
		},
		{
			name:         "EmptyLocalKeys",
			localKeys:    []phase0.BLSPubKey{},
			remoteKeys:   []phase0.BLSPubKey{{1, 2, 3}},
			expectedKeys: nil,
			listError:    false,
			expectError:  false,
		},
		{
			name: "EmptyRemoteKeys",
			localKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			remoteKeys: []phase0.BLSPubKey{},
			expectedKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			listError:   false,
			expectError: false,
		},
		{
			name:         "ListValidatorsError",
			localKeys:    []phase0.BLSPubKey{{1, 2, 3}},
			remoteKeys:   nil,
			expectedKeys: nil,
			listError:    true,
			expectError:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.resetMux()

			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)

				if tc.listError {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("Server error"))
					return
				}

				w.WriteHeader(http.StatusOK)
				respBytes, err := json.Marshal(tc.remoteKeys)
				require.NoError(t, err, "Failed to marshal response")
				w.Write(respBytes)
			})

			result, err := s.client.MissingKeys(context.Background(), tc.localKeys)

			if tc.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Unexpected error")

				// create sets to compare results regardless of order
				expectedSet := make(map[phase0.BLSPubKey]struct{}, len(tc.expectedKeys))
				for _, key := range tc.expectedKeys {
					expectedSet[key] = struct{}{}
				}

				resultSet := make(map[phase0.BLSPubKey]struct{}, len(result))
				for _, key := range result {
					resultSet[key] = struct{}{}
				}

				assert.Equal(t, expectedSet, resultSet)
			}

			assert.Equal(t, 1, s.serverHits)
		})
	}
}

func (s *SSVSignerClientSuite) TestNew() {
	t := s.T()

	testCases := []struct {
		name            string
		baseURL         string
		expectedBaseURL string
	}{
		{
			name:            "NormalURL",
			baseURL:         "http://example.com",
			expectedBaseURL: "http://example.com",
		},
		{
			name:            "URLWithTrailingSlash",
			baseURL:         "http://example.com/",
			expectedBaseURL: "http://example.com",
		},
		{
			name:            "Empty",
			baseURL:         "",
			expectedBaseURL: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewClient(tc.baseURL)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedBaseURL, client.baseURL)
			assert.NotNil(t, client.httpClient)

			logger, _ := zap.NewDevelopment()
			clientWithLogger, err := NewClient(tc.baseURL, WithLogger(logger))
			require.NoError(t, err)
			assert.Equal(t, tc.expectedBaseURL, clientWithLogger.baseURL)
			assert.Equal(t, logger, clientWithLogger.logger)
		})
	}
}

func TestSSVSignerClientSuite(t *testing.T) {
	suite.Run(t, new(SSVSignerClientSuite))
}

func TestCustomHTTPClient(t *testing.T) {
	t.Parallel()

	customClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns: 100,
		},
	}

	withCustomClient := func(client *Client) {
		client.httpClient = customClient
	}

	c, err := NewClient("http://example.com", withCustomClient)

	require.NoError(t, err)
	assert.Equal(t, customClient, c.httpClient)
}

func TestRequestErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("Could not create hijacker")
		}

		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}

		conn.Close()
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client, err := NewClient(server.URL, WithLogger(logger))
	require.NoError(t, err)

	err = client.AddValidators(context.Background(), ShareKeys{
		EncryptedPrivKey: []byte("test"),
		PubKey:           phase0.BLSPubKey{1, 1, 1},
	})
	assert.Error(t, err)

	err = client.RemoveValidators(context.Background(), phase0.BLSPubKey{1, 1, 1})
	assert.Error(t, err)

	_, err = client.Sign(context.Background(), phase0.BLSPubKey{1, 1, 1}, web3signer.SignRequest{})
	assert.Error(t, err)

	_, err = client.OperatorIdentity(context.Background())
	assert.Error(t, err)

	_, err = client.OperatorSign(context.Background(), []byte{1, 1, 1})
	assert.Error(t, err)
}

func TestResponseHandlingErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client, err := NewClient(server.URL, WithLogger(logger))
	require.NoError(t, err)

	err = client.AddValidators(context.Background(), ShareKeys{
		EncryptedPrivKey: []byte("test"),
		PubKey:           phase0.BLSPubKey{1, 1, 1},
	})
	assert.Error(t, err)

	err = client.RemoveValidators(context.Background(), phase0.BLSPubKey{1, 1, 1})
	assert.Error(t, err)
}

func TestNew(t *testing.T) {
	t.Parallel()

	logger, zapErr := zap.NewDevelopment()
	require.NoError(t, zapErr)

	exampleLogger := zap.NewExample()
	certData := []byte("")
	keyData := []byte("")
	caCertData := []byte("")

	testCases := []struct {
		name            string
		baseURL         string
		opts            []ClientOption
		expectedBaseURL string
		checkLogger     bool
		expectedLogger  *zap.Logger
		checkTLS        bool
	}{
		{
			name:            "NormalURL",
			baseURL:         "http://example.com",
			opts:            nil,
			expectedBaseURL: "http://example.com",
		},
		{
			name:            "URLWithTrailingSlash",
			baseURL:         "http://example.com/",
			expectedBaseURL: "http://example.com",
		},
		{
			name:            "Empty",
			baseURL:         "",
			expectedBaseURL: "",
		},
		{
			name:            "WithLogger",
			baseURL:         "http://localhost:9000",
			opts:            []ClientOption{WithLogger(logger)},
			expectedBaseURL: "http://localhost:9000",
			checkLogger:     true,
			expectedLogger:  logger,
		},
		{
			name:            "WithAllOptions",
			baseURL:         "https://test.example.com",
			opts:            []ClientOption{WithLogger(exampleLogger), WithClientTLSCertificates(certData, keyData, caCertData)},
			expectedBaseURL: "https://test.example.com",
			checkLogger:     true,
			expectedLogger:  exampleLogger,
			checkTLS:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(tc.baseURL, tc.opts...)
			require.NoError(t, err)
			require.NotNil(t, client)

			assert.Equal(t, tc.expectedBaseURL, client.baseURL)

			if tc.checkLogger && tc.expectedLogger != nil {
				assert.Equal(t, tc.expectedLogger, client.logger)
			} else {
				assert.NotNil(t, client.logger) // default noop logger
			}

			assert.NotNil(t, client.httpClient)
			assert.Equal(t, 30*time.Second, client.httpClient.Timeout)

			if tc.checkTLS {
				assert.NotNil(t, client.tlsConfig)
				transport, ok := client.httpClient.Transport.(*http.Transport)
				assert.True(t, ok)
				assert.NotNil(t, transport.TLSClientConfig)
			}
		})
	}
}

func Test_ShareDecryptionError(t *testing.T) {
	t.Parallel()

	var customErr error = ShareDecryptionError(errors.New("test error"))

	var shareDecryptionError ShareDecryptionError
	if !errors.As(customErr, &shareDecryptionError) {
		t.Errorf("shareDecryptionError was expected to be a ShareDecryptionError")
	}
}

func TestNewClient_TrimsTrailingSlashFromURL(t *testing.T) {
	t.Parallel()

	const inputURL = "https://test.example.com/"
	const expectedURL = "https://test.example.com"

	client, err := NewClient(inputURL)

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, expectedURL, client.baseURL)
}

func TestClientTLS(t *testing.T) {
	t.Parallel()

	caCert, _, serverCert, serverKey := testingutils.GenerateCertificates(t, "localhost")

	testCases := []struct {
		name           string
		serverTLS      bool
		clientCert     []byte
		clientKey      []byte
		caCert         []byte
		skipVerify     bool
		expectError    bool
		errorContains  string
		serverRequires bool
	}{
		{
			name:        "No TLS",
			serverTLS:   false,
			expectError: false,
		},
		{
			name:           "Server TLS with CA cert",
			serverTLS:      true,
			caCert:         caCert,
			expectError:    false,
			serverRequires: false,
		},
		{
			name:           "Server TLS with skipVerify",
			serverTLS:      true,
			skipVerify:     true,
			expectError:    false,
			serverRequires: false,
		},
		{
			name:           "Server TLS with no CA or skipVerify",
			serverTLS:      true,
			expectError:    true,
			errorContains:  "certificate is not trusted",
			serverRequires: false,
		},
		{
			name:           "Server TLS requiring client cert, client provides",
			serverTLS:      true,
			clientCert:     serverCert, // server cert is used as client cert
			clientKey:      serverKey,
			caCert:         caCert,
			expectError:    false,
			serverRequires: true,
		},
		{
			name:           "Server TLS requiring client cert, client doesn't provide",
			serverTLS:      true,
			caCert:         caCert,
			expectError:    true,
			errorContains:  "certificate",
			serverRequires: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var serverTLSConfig *tls.Config
			if tc.serverTLS {
				// setup server TLS config
				if tc.serverRequires {
					serverTLSConfig = testingutils.CreateServerTLSConfig(t, serverCert, serverKey, caCert, true)
				} else {
					serverTLSConfig = testingutils.CreateServerTLSConfig(t, serverCert, serverKey, nil, false)
				}
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == pathValidators {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("[]"))
				}
			})

			server := httptest.NewUnstartedServer(handler)
			if tc.serverTLS {
				server.TLS = serverTLSConfig
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()

			var clientOpts []ClientOption
			if tc.clientCert != nil && tc.clientKey != nil {
				clientOpts = append(clientOpts, WithClientTLSCertificates(tc.clientCert, tc.clientKey, tc.caCert))
			} else if tc.caCert != nil {
				clientOpts = append(clientOpts, WithClientTLSCertificates(nil, nil, tc.caCert))
			}
			if tc.skipVerify {
				clientOpts = append(clientOpts, WithClientInsecureSkipVerify())
			}

			client, err := NewClient(server.URL, clientOpts...)
			require.NoError(t, err)

			// test ListValidators which makes an HTTP request
			_, err = client.ListValidators(context.Background())

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClientTLSOptions(t *testing.T) {
	t.Parallel()

	caCert, _, clientCert, clientKey := testingutils.GenerateCertificates(t, "localhost")

	testCases := []struct {
		name              string
		clientCert        []byte
		clientKey         []byte
		caCert            []byte
		skipVerify        bool
		expectTLSConfig   bool
		expectCertificate bool
		expectRootCAs     bool
		expectSkipVerify  bool
	}{
		{
			name:            "No TLS options",
			expectTLSConfig: false,
		},
		{
			name:              "With client certificate",
			clientCert:        clientCert,
			clientKey:         clientKey,
			expectTLSConfig:   true,
			expectCertificate: true,
		},
		{
			name:            "With CA certificate",
			caCert:          caCert,
			expectTLSConfig: true,
			expectRootCAs:   true,
		},
		{
			name:             "With skip verify",
			skipVerify:       true,
			expectTLSConfig:  true,
			expectSkipVerify: true,
		},
		{
			name:              "With all options",
			clientCert:        clientCert,
			clientKey:         clientKey,
			caCert:            caCert,
			skipVerify:        true,
			expectTLSConfig:   true,
			expectCertificate: true,
			expectRootCAs:     true,
			expectSkipVerify:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clientOpts []ClientOption

			if tc.clientCert != nil && tc.clientKey != nil {
				if tc.caCert != nil {
					clientOpts = append(clientOpts, WithClientTLSCertificates(tc.clientCert, tc.clientKey, tc.caCert))
				} else {
					clientOpts = append(clientOpts, WithClientTLSCertificates(tc.clientCert, tc.clientKey, nil))
				}
			} else if tc.caCert != nil {
				clientOpts = append(clientOpts, WithClientTLSCertificates(nil, nil, tc.caCert))
			}

			if tc.skipVerify {
				clientOpts = append(clientOpts, WithClientInsecureSkipVerify())
			}

			client, err := NewClient("https://example.com", clientOpts...)
			require.NoError(t, err)

			transport, ok := client.httpClient.Transport.(*http.Transport)
			if tc.expectTLSConfig {
				require.True(t, ok)
				require.NotNil(t, transport)
				require.NotNil(t, transport.TLSClientConfig)

				if tc.expectCertificate {
					if tc.name == "With all options" {
						require.Greater(t, len(transport.TLSClientConfig.Certificates), 0)
					} else {
						require.Len(t, transport.TLSClientConfig.Certificates, 1)
					}
				}

				if tc.expectRootCAs {
					require.NotNil(t, transport.TLSClientConfig.RootCAs)
				}

				require.Equal(t, tc.expectSkipVerify, transport.TLSClientConfig.InsecureSkipVerify)
			}
		})
	}
}

func TestTLSConfigCLIIntegration(t *testing.T) {
	t.Parallel()

	// Create temporary files for testing
	clientCertFile, err := os.CreateTemp("", "client-cert-*.pem")
	require.NoError(t, err)
	defer os.Remove(clientCertFile.Name())

	clientKeyFile, err := os.CreateTemp("", "client-key-*.pem")
	require.NoError(t, err)
	defer os.Remove(clientKeyFile.Name())

	caCertFile, err := os.CreateTemp("", "ca-cert-*.pem")
	require.NoError(t, err)
	defer os.Remove(caCertFile.Name())

	// Generate test certificates
	caCert, _, clientCert, clientKey := testingutils.GenerateCertificates(t, "localhost")

	// Write test data to temp files
	err = os.WriteFile(clientCertFile.Name(), clientCert, 0644)
	require.NoError(t, err)

	err = os.WriteFile(clientKeyFile.Name(), clientKey, 0644)
	require.NoError(t, err)

	err = os.WriteFile(caCertFile.Name(), caCert, 0644)
	require.NoError(t, err)

	// Test cases that simulate CLI configuration
	testCases := []struct {
		name          string
		tlsConfig     ssvsignertls.ClientConfig
		expectOptions int
		expectError   bool
	}{
		{
			name: "Full TLS configuration",
			tlsConfig: ssvsignertls.ClientConfig{
				ClientCertFile:           clientCertFile.Name(),
				ClientKeyFile:            clientKeyFile.Name(),
				ClientCACertFile:         caCertFile.Name(),
				ClientInsecureSkipVerify: false,
			},
			expectOptions: 1, // One option with the full TLS config
			expectError:   false,
		},
		{
			name: "CA cert only",
			tlsConfig: ssvsignertls.ClientConfig{
				ClientCACertFile: caCertFile.Name(),
			},
			expectOptions: 1, // One option with CA cert only
			expectError:   false,
		},
		{
			name: "InsecureSkipVerify only",
			tlsConfig: ssvsignertls.ClientConfig{
				ClientInsecureSkipVerify: true,
			},
			expectOptions: 1, // One option with InsecureSkipVerify
			expectError:   false,
		},
		{
			name:          "No TLS configuration",
			tlsConfig:     ssvsignertls.ClientConfig{},
			expectOptions: 0, // No options when no TLS config is provided
			expectError:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.expectError {
				require.Error(t, err)
				return
			}

			if tc.expectOptions == 0 {
				return // No need to check further
			}

			logger, _ := zap.NewDevelopment()
			allOptions := append([]ClientOption{WithLogger(logger)},
				WithClientTLSCertificates(clientCert, clientKey, caCert),
				WithClientInsecureSkipVerify())

			// Use localhost as a dummy endpoint
			client, err := NewClient("https://localhost:8000", allOptions...)
			require.NoError(t, err)
			require.NotNil(t, client)

			transport, ok := client.httpClient.Transport.(*http.Transport)
			require.True(t, ok)
			require.NotNil(t, transport)
			require.NotNil(t, transport.TLSClientConfig)

			// Verify specific TLS settings
			if tc.tlsConfig.ClientInsecureSkipVerify {
				require.True(t, transport.TLSClientConfig.InsecureSkipVerify)
			}

			if tc.tlsConfig.ClientCACertFile != "" {
				require.NotNil(t, transport.TLSClientConfig.RootCAs)
			}

			if tc.tlsConfig.ClientCertFile != "" && tc.tlsConfig.ClientKeyFile != "" {
				require.NotEmpty(t, transport.TLSClientConfig.Certificates)
			}
		})
	}
}
