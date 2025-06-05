package ssvsigner

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"unicode"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/internal/mocks"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type ServerTestSuite struct {
	suite.Suite
	logger          *zap.Logger
	operatorPrivKey *mocks.TestOperatorPrivateKey
	remoteSigner    *mocks.TestRemoteSigner
	server          *Server
	pubKey          *mocks.TestOperatorPublicKey
}

func (s *ServerTestSuite) SetupTest() {
	var err error
	s.logger, err = zap.NewDevelopment()
	s.Require().NoError(err)

	err = bls.Init(bls.BLS12_381)
	s.Require().NoError(err)

	s.pubKey = &mocks.TestOperatorPublicKey{
		PubKeyBase64: "test_pubkey_base64",
	}

	s.operatorPrivKey = &mocks.TestOperatorPrivateKey{
		Base64Value:      "test_operator_key_base64",
		BytesValue:       []byte("test_bytes"),
		StorageHashValue: "test_storage_hash",
		EkmHashValue:     "test_ekm_hash",
		DecryptResult:    []byte("decrypted_data"),
		PublicKey:        s.pubKey,
		SignResult:       []byte("signature_bytes"),
	}

	s.remoteSigner = &mocks.TestRemoteSigner{
		ListKeysResult: []phase0.BLSPubKey{{1, 2, 3}, {4, 5, 6}},
		ImportResult: web3signer.ImportKeystoreResponse{
			Data: []web3signer.KeyManagerResponseData{
				{
					Status: web3signer.StatusImported,
				},
			},
		},
		DeleteResult: web3signer.DeleteKeystoreResponse{
			Data: []web3signer.KeyManagerResponseData{
				{
					Status: web3signer.StatusDeleted,
				},
			},
		},
		SignResult: web3signer.SignResponse{
			Signature: phase0.BLSSignature{1, 1, 1},
		},
	}

	s.server = NewServer(s.logger, s.operatorPrivKey, s.remoteSigner)
}

func (s *ServerTestSuite) ServeHTTP(method, path string, body []byte) (*fasthttp.Response, error) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod(method)
	req.SetRequestURI(path)

	if len(body) > 0 {
		req.SetBody(body)
		req.Header.SetContentType("application/json")
	}

	resp := fasthttp.AcquireResponse()

	ctx := &fasthttp.RequestCtx{}
	ctx.Init(req, nil, nil)

	s.server.Handler()(ctx)

	ctx.Response.CopyTo(resp)

	return resp, nil
}

func (s *ServerTestSuite) TestListValidators() {
	t := s.T()

	t.Run("success", func(t *testing.T) {
		resp, err := s.ServeHTTP("GET", pathValidators, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

		var response []string
		err = json.Unmarshal(resp.Body(), &response)
		require.NoError(t, err)

		expected := []string{
			"0x010203000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
			"0x040506000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		}
		assert.Equal(t, expected, response)
	})

	t.Run("error", func(t *testing.T) {
		s.remoteSigner.ListKeysError = errors.New("remote signer error")
		resp, err := s.ServeHTTP("GET", pathValidators, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.ListKeysError = nil
	})
}

func (s *ServerTestSuite) TestAddValidator() {
	t := s.T()

	sk := new(bls.SecretKey)
	sk.SetByCSPRNG()
	pubKey := sk.GetPublicKey().Serialize()

	validBlsKey := "0x" + hex.EncodeToString(sk.Serialize())
	s.operatorPrivKey.DecryptResult = []byte(validBlsKey)

	request := AddValidatorRequest{
		ShareKeys: []ShareKeys{
			{
				EncryptedPrivKey: []byte("encrypted_key"),
				PubKey:           phase0.BLSPubKey(pubKey),
			},
		},
	}
	reqBody, err := json.Marshal(request)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

		var response web3signer.ImportKeystoreResponse
		err = json.Unmarshal(resp.Body(), &response)
		require.NoError(t, err)
		assert.Equal(t, web3signer.ImportKeystoreResponse{Data: []web3signer.KeyManagerResponseData{{Status: web3signer.StatusImported}}}, response)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathValidators, []byte("{invalid json}"))
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())
	})

	t.Run("empty request body", func(t *testing.T) {
		emptyRequest := AddValidatorRequest{
			ShareKeys: []ShareKeys{},
		}
		emptyReqBody, err := json.Marshal(emptyRequest)
		require.NoError(t, err)
		resp, err := s.ServeHTTP("POST", pathValidators, emptyReqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	})

	t.Run("invalid public key", func(t *testing.T) {
		invalidPubKeyRequest := AddValidatorRequest{
			ShareKeys: []ShareKeys{
				{
					EncryptedPrivKey: []byte("encrypted_key"),
					PubKey:           phase0.BLSPubKey{},
				},
			},
		}
		invalidPubKeyReqBody, err := json.Marshal(invalidPubKeyRequest)
		require.NoError(t, err)
		resp, err := s.ServeHTTP("POST", pathValidators, invalidPubKeyReqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())
	})

	t.Run("invalid private key", func(t *testing.T) {
		s.operatorPrivKey.DecryptResult = []byte{}

		invalidPrivKeyRequest := AddValidatorRequest{
			ShareKeys: []ShareKeys{
				{
					EncryptedPrivKey: []byte{},
					PubKey:           phase0.BLSPubKey(pubKey),
				},
			},
		}
		invalidPrivKeyReqBody, err := json.Marshal(invalidPrivKeyRequest)
		require.NoError(t, err)
		resp, err := s.ServeHTTP("POST", pathValidators, invalidPrivKeyReqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.DecryptResult = []byte(validBlsKey)
	})

	t.Run("decryption error", func(t *testing.T) {
		s.operatorPrivKey.DecryptError = errors.New("decryption error")
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.DecryptError = nil
	})

	t.Run("invalid decrypt result", func(t *testing.T) {
		s.operatorPrivKey.DecryptResult = []byte("not-a-hex-string")
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.DecryptResult = []byte(validBlsKey)
	})

	t.Run("different decrypt result", func(t *testing.T) {
		differentSk := new(bls.SecretKey)
		differentSk.SetByCSPRNG()
		differentBlsKey := "0x" + hex.EncodeToString(differentSk.Serialize())
		s.operatorPrivKey.DecryptResult = []byte(differentBlsKey)

		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.DecryptResult = []byte(validBlsKey)
	})

	t.Run("import error", func(t *testing.T) {
		s.remoteSigner.ImportError = errors.New("import error")
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.ImportError = nil
	})
}

func (s *ServerTestSuite) TestRemoveValidator() {
	t := s.T()

	request := web3signer.DeleteKeystoreRequest{
		Pubkeys: []phase0.BLSPubKey{{1, 2, 3}, {4, 5, 6}},
	}
	reqBody, err := json.Marshal(request)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		resp, err := s.ServeHTTP("DELETE", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

		var response web3signer.DeleteKeystoreResponse
		err = json.Unmarshal(resp.Body(), &response)
		require.NoError(t, err)
		assert.Equal(t, web3signer.DeleteKeystoreResponse{Data: []web3signer.KeyManagerResponseData{{Status: web3signer.StatusDeleted}}}, response)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		resp, err := s.ServeHTTP("DELETE", pathValidators, []byte("{invalid json}"))
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())
	})

	t.Run("empty request", func(t *testing.T) {
		emptyRequest := web3signer.DeleteKeystoreRequest{
			Pubkeys: []phase0.BLSPubKey{},
		}
		emptyReqBody, err := json.Marshal(emptyRequest)
		require.NoError(t, err)
		resp, err := s.ServeHTTP("DELETE", pathValidators, emptyReqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	})

	t.Run("custom delete result", func(t *testing.T) {
		s.remoteSigner.DeleteResult = web3signer.DeleteKeystoreResponse{Data: []web3signer.KeyManagerResponseData{{Status: web3signer.StatusDeleted}, {Status: web3signer.StatusNotFound}}}
		resp, err := s.ServeHTTP("DELETE", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	})

	t.Run("custom delete error", func(t *testing.T) {
		s.remoteSigner.DeleteResult = web3signer.DeleteKeystoreResponse{Data: []web3signer.KeyManagerResponseData{{Status: web3signer.StatusDeleted}}}

		s.remoteSigner.DeleteError = errors.New("remote signer error")
		resp, err := s.ServeHTTP("DELETE", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.DeleteError = nil
	})
}

func (s *ServerTestSuite) TestSignValidator() {
	t := s.T()

	pubKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	signPayload := web3signer.SignRequest{
		Type: web3signer.TypeAttestation,
	}

	reqBody, err := json.Marshal(signPayload)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathValidatorsSign+pubKeyHex, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

		const expectedSignature = `{"signature":"0x010101000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"}`

		assert.JSONEq(t, expectedSignature, string(resp.Body()))
	})

	t.Run("invalid JSON", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathValidatorsSign+pubKeyHex, []byte("{invalid json}"))
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())
	})

	t.Run("no public key", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathValidatorsSign, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusNotFound, resp.StatusCode())
	})

	t.Run("invalid public key", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathValidatorsSign+"invalid", reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())
	})

	t.Run("remote signer error", func(t *testing.T) {
		s.remoteSigner.SignError = errors.New("remote signer error")
		resp, err := s.ServeHTTP("POST", pathValidatorsSign+pubKeyHex, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.SignError = nil
	})
}

func (s *ServerTestSuite) TestOperatorIdentity() {
	t := s.T()

	t.Run("success", func(t *testing.T) {
		resp, err := s.ServeHTTP("GET", pathOperatorIdentity, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
		assert.Equal(t, "test_pubkey_base64", string(resp.Body()))
	})

	t.Run("error", func(t *testing.T) {
		s.pubKey.Base64Error = errors.New("base64 error")
		resp, err := s.ServeHTTP("GET", pathOperatorIdentity, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.pubKey.Base64Error = nil
	})
}

func (s *ServerTestSuite) TestOperatorSign() {
	t := s.T()

	messageToSign := []byte("message_to_sign")

	t.Run("success", func(t *testing.T) {
		resp, err := s.ServeHTTP("POST", pathOperatorSign, messageToSign)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
		assert.Equal(t, []byte("signature_bytes"), resp.Body())
	})

	t.Run("error", func(t *testing.T) {
		s.operatorPrivKey.SignError = errors.New("sign error")
		resp, err := s.ServeHTTP("POST", pathOperatorSign, messageToSign)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.operatorPrivKey.SignError = nil
	})
}

func (s *ServerTestSuite) TestRouting() {
	t := s.T()

	t.Run("wrong path", func(t *testing.T) {
		resp, err := s.ServeHTTP("GET", "/non-existent-route", nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusNotFound, resp.StatusCode())
	})

	t.Run("wrong method", func(t *testing.T) {
		resp, err := s.ServeHTTP("PUT", pathValidators, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusMethodNotAllowed, resp.StatusCode())
	})
}

func (s *ServerTestSuite) TestHelperMethods() {
	t := s.T()

	ctx := &fasthttp.RequestCtx{}

	t.Run("writeString", func(t *testing.T) {
		s.server.writeString(ctx, zap.L(), "test string")
		assert.Equal(t, "test string", string(ctx.Response.Body()))
	})

	ctx.Response.Reset()

	t.Run("writeBytes", func(t *testing.T) {
		s.server.writeBytes(ctx, zap.L(), []byte("test bytes"))
		assert.Equal(t, "test bytes", string(ctx.Response.Body()))
	})

	ctx.Response.Reset()

	t.Run("writeJSONErr", func(t *testing.T) {
		s.server.writeJSONErr(ctx, zap.L(), fasthttp.StatusInternalServerError, errors.New("test error"))
		assert.JSONEq(t, `{"message":"test error"}`, string(ctx.Response.Body()))
	})

	ctx.Response.Reset()

	t.Run("writeJSON", func(t *testing.T) {
		s.server.writeJSON(ctx, zap.L(), map[string]any{"a": 1, "b": 2})
		assert.Equal(t, `{"a":1,"b":2}`, string(ctx.Response.Body()))
	})
}

func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerTestSuite))
}

func TestGenerateRandomPassword(t *testing.T) {
	server := &Server{}

	testCases := []struct {
		length int
	}{
		{8},
		{12},
		{16},
	}

	for _, test := range testCases {
		t.Run(fmt.Sprintf("PasswordLength%d", test.length), func(t *testing.T) {
			password, err := server.generateRandomPassword(test.length)
			require.NoError(t, err)

			require.Equal(t, test.length, len(password), "Password length is incorrect")

			for _, char := range password {
				require.True(t, unicode.IsLetter(char) || unicode.IsDigit(char), "Password contains invalid character")
			}
		})
	}

	password1, err := server.generateRandomPassword(12)
	require.NoError(t, err)

	password2, err := server.generateRandomPassword(12)
	require.NoError(t, err)

	assert.NotEqual(t, password1, password2, "Passwords should be different")
}
