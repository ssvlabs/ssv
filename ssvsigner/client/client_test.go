package ssvsignerclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/server"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

func Test_ShareDecryptionError(t *testing.T) {
	var customErr error = ShareDecryptionError(errors.New("test error"))

	var shareDecryptionError ShareDecryptionError
	if !errors.As(customErr, &shareDecryptionError) {
		t.Errorf("shareDecryptionError was expected to be a ShareDecryptionError")
	}
}

type SSVSignerClientSuite struct {
	suite.Suite
	server     *httptest.Server
	client     *SSVSignerClient
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
	s.client = New(s.server.URL, WithLogger(s.logger))
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
		expectedResponse   server.AddValidatorResponse
		expectedResult     []Status
		expectError        bool
		isDecryptionError  bool
	}{
		{
			name: "Success",
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted1"),
					PublicKey:        []byte("pubkey1"),
				},
				{
					EncryptedPrivKey: []byte("encrypted2"),
					PublicKey:        []byte("pubkey2"),
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedResponse: server.AddValidatorResponse{
				Statuses: []Status{StatusImported, StatusDuplicated},
			},
			expectedResult: []Status{StatusImported, StatusDuplicated},
			expectError:    false,
		},
		{
			name: "DecryptionError",
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("bad_encrypted"),
					PublicKey:        []byte("pubkey"),
				},
			},
			expectedStatusCode: http.StatusUnprocessableEntity,
			expectedResponse:   server.AddValidatorResponse{},
			expectError:        true,
			isDecryptionError:  true,
		},
		{
			name: "ServerError",
			shares: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted"),
					PublicKey:        []byte("pubkey"),
				},
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   server.AddValidatorResponse{},
			expectError:        true,
		},
		{
			name:               "NoShares",
			shares:             []ShareKeys{},
			expectedStatusCode: http.StatusOK,
			expectedResponse: server.AddValidatorResponse{
				Statuses: []Status{},
			},
			expectedResult: []Status{},
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc("/v1/validators/add", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)

				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req server.AddValidatorRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				assert.Len(t, req.ShareKeys, len(tc.shares))
				for i, share := range tc.shares {
					assert.Equal(t, hex.EncodeToString(share.EncryptedPrivKey), req.ShareKeys[i].EncryptedPrivKey)
					assert.Equal(t, hex.EncodeToString(share.PublicKey), req.ShareKeys[i].PublicKey)
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
		pubKeys            [][]byte
		expectedStatusCode int
		expectedResponse   server.RemoveValidatorResponse
		expectedResult     []Status
		expectError        bool
	}{
		{
			name: "Success",
			pubKeys: [][]byte{
				[]byte("pubkey1"),
				[]byte("pubkey2"),
			},
			expectedStatusCode: http.StatusOK,
			expectedResponse: server.RemoveValidatorResponse{
				Statuses: []Status{StatusDeleted, StatusNotFound},
			},
			expectedResult: []Status{StatusDeleted, StatusNotFound},
			expectError:    false,
		},
		{
			name:               "ServerError",
			pubKeys:            [][]byte{[]byte("pubkey")},
			expectedStatusCode: http.StatusInternalServerError,
			expectedResponse:   server.RemoveValidatorResponse{},
			expectError:        true,
		},
		{
			name:               "NoPubKeys",
			pubKeys:            [][]byte{},
			expectedStatusCode: http.StatusOK,
			expectedResponse: server.RemoveValidatorResponse{
				Statuses: []Status{},
			},
			expectedResult: []Status{},
			expectError:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc("/v1/validators/remove", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)

				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req server.RemoveValidatorRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				assert.Len(t, req.PublicKeys, len(tc.pubKeys))
				for i, pubKey := range tc.pubKeys {
					assert.Equal(t, hex.EncodeToString(pubKey), req.PublicKeys[i])
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

func (s *SSVSignerClientSuite) TestSign() {
	t := s.T()

	samplePubKey := []byte("sample_pubkey")
	samplePayload := web3signer.SignRequest{
		Type: "ATTESTATION",
	}
	expectedSignature := []byte("signed_data")

	testCases := []struct {
		name               string
		pubKey             []byte
		payload            web3signer.SignRequest
		expectedStatusCode int
		expectedResult     []byte
		expectError        bool
	}{
		{
			name:               "Success",
			pubKey:             samplePubKey,
			payload:            samplePayload,
			expectedStatusCode: http.StatusOK,
			expectedResult:     expectedSignature,
			expectError:        false,
		},
		{
			name:               "ServerError",
			pubKey:             samplePubKey,
			payload:            samplePayload,
			expectedStatusCode: http.StatusInternalServerError,
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc("/v1/validators/sign/"+hex.EncodeToString(tc.pubKey), func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)

				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				body, err := io.ReadAll(r.Body)
				require.NoError(t, err, "Failed to read request body")
				defer r.Body.Close()

				var req web3signer.SignRequest
				err = json.Unmarshal(body, &req)
				require.NoError(t, err, "Failed to unmarshal request body")

				assert.Equal(t, tc.payload.Type, req.Type)

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					w.Write(tc.expectedResult)
				} else {
					w.Write([]byte("Server error"))
				}
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

func (s *SSVSignerClientSuite) TestGetOperatorIdentity() {
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
			s.mux.HandleFunc("/v1/operator/identity", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)

				w.WriteHeader(tc.expectedStatusCode)
				if tc.expectedStatusCode == http.StatusOK {
					w.Write([]byte(tc.expectedResult))
				} else {
					w.Write([]byte("Server error"))
				}
			})

			result, err := s.client.GetOperatorIdentity(context.Background())

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
			name:               "Success",
			payload:            samplePayload,
			expectedStatusCode: http.StatusOK,
			expectedResult:     expectedSignature,
			expectError:        false,
		},
		{
			name:               "ServerError",
			payload:            samplePayload,
			expectedStatusCode: http.StatusInternalServerError,
			expectError:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s.mux = http.NewServeMux()
			s.mux.HandleFunc("/v1/operator/sign", func(w http.ResponseWriter, r *http.Request) {
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
			client := New(tc.baseURL)
			assert.Equal(t, tc.expectedBaseURL, client.baseURL)
			assert.NotNil(t, client.httpClient)

			logger, _ := zap.NewDevelopment()
			clientWithLogger := New(tc.baseURL, WithLogger(logger))
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

	withCustomClient := func(client *SSVSignerClient) {
		client.httpClient = customClient
	}

	c := New("http://example.com", withCustomClient)

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

	client := New(server.URL)

	_, err := client.AddValidators(context.Background(), ShareKeys{
		EncryptedPrivKey: []byte("test"),
		PublicKey:        []byte("test"),
	})
	assert.Error(t, err)

	_, err = client.RemoveValidators(context.Background(), []byte("test"))
	assert.Error(t, err)

	_, err = client.Sign(context.Background(), []byte("test"), web3signer.SignRequest{})
	assert.Error(t, err)

	_, err = client.GetOperatorIdentity(context.Background())
	assert.Error(t, err)

	_, err = client.OperatorSign(context.Background(), []byte("test"))
	assert.Error(t, err)
}

func TestResponseHandlingErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := New(server.URL)

	_, err := client.AddValidators(context.Background(), ShareKeys{
		EncryptedPrivKey: []byte("test"),
		PublicKey:        []byte("test"),
	})
	assert.Error(t, err)

	_, err = client.RemoveValidators(context.Background(), []byte("test"))
	assert.Error(t, err)
}
