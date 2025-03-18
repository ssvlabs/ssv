package ssvsigner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

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
	require.NoError(s.T(), err, "Failed to create logger")

	s.mux = http.NewServeMux()
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serverHits++
		s.mux.ServeHTTP(w, r)
	}))
	s.client = NewClient(s.server.URL, WithLogger(s.logger))
}

func (s *SSVSignerClientSuite) TearDownTest() {
	s.server.Close()
	s.serverHits = 0
}

func (s *SSVSignerClientSuite) TestAddValidators() {
	t := s.T()

	testCases := []struct {
		name               string
		shares             []ShareKeys
		expectedStatusCode int
		expectedResponse   AddValidatorResponse
		expectedResult     []web3signer.Status
		expectError        bool
		isDecryptionError  bool
	}{
		{
			name: "Success", // TODO: fix
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted1"),
					PublicKey:        phase0.BLSPubKey{1, 2, 3},
				},
				{
					EncryptedPrivKey: []byte("encrypted2"),
					PublicKey:        phase0.BLSPubKey{4, 5, 6},
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedResponse: AddValidatorResponse{
				Statuses: []web3signer.Status{web3signer.StatusImported, web3signer.StatusDuplicated},
			},
			expectedResult: []web3signer.Status{web3signer.StatusImported, web3signer.StatusDuplicated},
			expectError:    false,
		},
		{
			name: "DecryptionError", // TODO: fix
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("bad_encrypted"),
					PublicKey:        phase0.BLSPubKey{1, 2, 3},
				},
			},
			expectedStatusCode: http.StatusUnprocessableEntity,
			expectedResponse:   AddValidatorResponse{},
			expectError:        true,
			isDecryptionError:  true,
		},
		{
			name: "ServerError", // TODO: fix
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted"),
					PublicKey:        phase0.BLSPubKey{1, 2, 3},
				},
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   AddValidatorResponse{},
			expectError:        true,
		},
		{
			name:               "NoShares",
			shares:             []ShareKeys{},
			expectedStatusCode: http.StatusOK,
			expectedResponse: AddValidatorResponse{
				Statuses: []web3signer.Status{},
			},
			expectedResult: []web3signer.Status{},
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)

				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req AddValidatorRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				assert.Len(t, req.ShareKeys, len(tc.shares))
				for i, share := range tc.shares {
					assert.Equal(t, hex.EncodeToString(share.EncryptedPrivKey), req.ShareKeys[i].EncryptedPrivKey)
					assert.Equal(t, hex.EncodeToString(share.PublicKey[:]), req.ShareKeys[i].PublicKey)
				}

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusUnprocessableEntity {
					w.Write([]byte("Decryption error"))
				} else if tc.expectedStatusCode == http.StatusOK {
					respBytes, err := json.Marshal(tc.expectedResponse)
					require.NoError(t, err, "Failed to marshal response")
					w.Write(respBytes)
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.AddValidators(context.Background(), tc.shares...)

			if tc.expectError {
				assert.Error(t, err, "Expected an error")
				if tc.isDecryptionError {
					var decryptErr ShareDecryptionError
					assert.True(t, errors.As(err, &decryptErr), "Expected a ShareDecryptionError")
				}
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}

			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
		s.serverHits = 0
	}
}

func (s *SSVSignerClientSuite) TestRemoveValidators() {
	t := s.T()

	testCases := []struct {
		name               string
		pubKeys            []phase0.BLSPubKey
		expectedStatusCode int
		expectedResponse   RemoveValidatorResponse
		expectedResult     []web3signer.Status
		expectError        bool
	}{
		{
			name: "Success",
			pubKeys: []phase0.BLSPubKey{
				{1, 2, 3},
				{4, 5, 6},
			},
			expectedStatusCode: http.StatusOK,
			expectedResponse: RemoveValidatorResponse{
				Statuses: []web3signer.Status{web3signer.StatusDeleted, web3signer.StatusNotFound},
			},
			expectedResult: []web3signer.Status{web3signer.StatusDeleted, web3signer.StatusNotFound},
			expectError:    false,
		},
		{
			name: "ServerError",
			pubKeys: []phase0.BLSPubKey{
				{1, 2, 3},
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   RemoveValidatorResponse{},
			expectError:        true,
		},
		{
			name:               "NoPubKeys",
			pubKeys:            []phase0.BLSPubKey{},
			expectedStatusCode: http.StatusOK,
			expectedResponse: RemoveValidatorResponse{
				Statuses: []web3signer.Status{},
			},
			expectedResult: []web3signer.Status{},
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)

				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req RemoveValidatorRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				assert.Len(t, req.PublicKeys, len(tc.pubKeys))
				for i, pubKey := range tc.pubKeys {
					assert.Equal(t, pubKey, req.PublicKeys[i])
				}

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					respBytes, err := json.Marshal(tc.expectedResponse)
					require.NoError(t, err, "Failed to marshal response")
					w.Write(respBytes)
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.RemoveValidators(context.Background(), tc.pubKeys...)

			if tc.expectError {
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}

			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
		s.serverHits = 0
	}
}

func (s *SSVSignerClientSuite) TestListValidators() {
	t := s.T()

	testCases := []struct {
		name               string
		expectedStatusCode int
		expectedResponse   ListValidatorsResponse
		expectedResult     []string
		expectError        bool
	}{
		{
			name:               "Success", // TODO: fix
			expectedStatusCode: http.StatusOK,
			expectedResponse: ListValidatorsResponse{
				phase0.BLSPubKey{1, 2, 3},
				phase0.BLSPubKey{4, 5, 6},
			},
			expectedResult: []string{
				"0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			},
			expectError: false,
		},
		{
			name:               "EmptyList", // TODO: fix
			expectedStatusCode: http.StatusOK,
			expectedResponse:   ListValidatorsResponse{},
			expectedResult:     []string{},
			expectError:        false,
		},
		{
			name:               "ServerError",
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   nil,
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc(pathValidators, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					respBytes, err := json.Marshal(tc.expectedResponse)
					require.NoError(t, err, "Failed to marshal response")
					w.Write(respBytes)
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.ListValidators(context.Background())

			if tc.expectError {
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}

			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
		s.serverHits = 0
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
			responseBody:       "0x1234567890abcdef",
			expectedResult:     phase0.BLSSignature{1, 1, 1, 1},
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
			s.mux = http.NewServeMux()
			s.mux.HandleFunc(pathValidatorsSign+tc.pubKey.String(), func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var req web3signer.SignRequest
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				w.WriteHeader(tc.expectedStatusCode)
				w.Write([]byte(tc.responseBody))
			})

			result, err := s.client.Sign(context.Background(), tc.pubKey, tc.payload)

			if tc.expectError {
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}

			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
		s.serverHits = 0
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
			s.mux = http.NewServeMux()
			s.mux.HandleFunc(pathOperatorIdentity, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					w.Write([]byte(tc.expectedResult))
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.OperatorIdentity(context.Background())

			if tc.expectError {
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}

			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
		s.serverHits = 0
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
			s.mux = http.NewServeMux()
			s.mux.HandleFunc(pathOperatorSign, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)

				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

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
				assert.Error(t, err, "Expected an error")
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.Equal(t, tc.expectedResult, result)
			}

			assert.Equal(t, 1, s.serverHits, "Expected server to be hit once")
		})
		s.serverHits = 0
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
			client := NewClient(tc.baseURL)
			assert.Equal(t, tc.expectedBaseURL, client.baseURL)
			assert.NotNil(t, client.httpClient)

			logger, _ := zap.NewDevelopment()
			clientWithLogger := NewClient(tc.baseURL, WithLogger(logger))
			assert.Equal(t, tc.expectedBaseURL, clientWithLogger.baseURL)
			assert.Equal(t, logger, clientWithLogger.logger)
		})
	}
}

func TestSSVSignerClientSuite(t *testing.T) {
	suite.Run(t, new(SSVSignerClientSuite))
}

func TestCustomHTTPClient(t *testing.T) {
	customClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns: 100,
		},
	}

	withCustomClient := func(client *Client) {
		client.httpClient = customClient
	}

	c := NewClient("http://example.com", withCustomClient)

	assert.Equal(t, customClient, c.httpClient)
}

func TestRequestErrors(t *testing.T) {
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

	client := NewClient(server.URL)

	_, err := client.AddValidators(context.Background(), ShareKeys{
		EncryptedPrivKey: []byte("test"),
		PublicKey:        phase0.BLSPubKey{1, 1, 1},
	})
	assert.Error(t, err)

	_, err = client.RemoveValidators(context.Background(), phase0.BLSPubKey{1, 1, 1})
	assert.Error(t, err)

	_, err = client.Sign(context.Background(), phase0.BLSPubKey{1, 1, 1}, web3signer.SignRequest{})
	assert.Error(t, err)

	_, err = client.OperatorIdentity(context.Background())
	assert.Error(t, err)

	_, err = client.OperatorSign(context.Background(), []byte{1, 1, 1})
	assert.Error(t, err)
}

func TestResponseHandlingErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewClient(server.URL)

	_, err := client.AddValidators(context.Background(), ShareKeys{
		EncryptedPrivKey: []byte("test"),
		PublicKey:        phase0.BLSPubKey{1, 1, 1},
	})
	assert.Error(t, err)

	_, err = client.RemoveValidators(context.Background(), phase0.BLSPubKey{1, 1, 1})
	assert.Error(t, err)
}

func TestNew(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	testCases := []struct {
		name    string
		baseURL string
		opts    []ClientOption
	}{
		{
			name:    "WithoutOptions",
			baseURL: "http://localhost:9000",
			opts:    nil,
		},
		{
			name:    "WithLogger",
			baseURL: "http://localhost:9000",
			opts:    []ClientOption{WithLogger(logger)},
		},
		{
			name:    "WithTrailingSlash",
			baseURL: "http://localhost:9000/",
			opts:    []ClientOption{WithLogger(logger)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(tc.baseURL, tc.opts...)

			expectedBaseURL := strings.TrimRight(tc.baseURL, "/")
			assert.Equal(t, expectedBaseURL, client.baseURL)

			if len(tc.opts) > 0 {
				assert.NotNil(t, client.logger)
			} else {
				assert.Nil(t, client.logger)
			}

			assert.NotNil(t, client.httpClient)
			assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
		})
	}
}

func Test_ShareDecryptionError(t *testing.T) {
	var customErr error = ShareDecryptionError(errors.New("test error"))

	var shareDecryptionError ShareDecryptionError
	if !errors.As(customErr, &shareDecryptionError) {
		t.Errorf("shareDecryptionError was expected to be a ShareDecryptionError")
	}
}
