package ssvsigner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type ServerTestSuite struct {
	suite.Suite
	logger          *zap.Logger
	operatorPrivKey *testOperatorPrivateKey
	remoteSigner    *testRemoteSigner
	server          *Server
	password        string
	pubKey          *testOperatorPublicKey
}

func (s *ServerTestSuite) SetupTest() {
	var err error
	s.logger, err = zap.NewDevelopment()
	s.Require().NoError(err)

	err = bls.Init(bls.BLS12_381)
	s.Require().NoError(err)

	s.pubKey = &testOperatorPublicKey{
		pubKeyBase64: "test_pubkey_base64",
	}

	s.operatorPrivKey = &testOperatorPrivateKey{
		base64Value:   "test_operator_key_base64",
		bytesValue:    []byte("test_bytes"),
		storageHash:   "test_storage_hash",
		ekmHash:       "test_ekm_hash",
		decryptResult: []byte("decrypted_data"),
		publicKey:     s.pubKey,
		signResult:    []byte("signature_bytes"),
	}

	s.remoteSigner = &testRemoteSigner{
		listKeysResult: []phase0.BLSPubKey{{1, 2, 3}, {4, 5, 6}},
		importResult: web3signer.ImportKeystoreResponse{
			Data: []web3signer.KeyManagerResponseData{
				{
					Status: web3signer.StatusImported,
				},
			}},
		deleteResult: web3signer.DeleteKeystoreResponse{
			Data: []web3signer.KeyManagerResponseData{
				{
					Status: web3signer.StatusDeleted,
				},
			}},
		signResult: web3signer.SignResponse{
			Signature: phase0.BLSSignature{1, 1, 1},
		},
	}

	s.password = "testpassword"

	s.server = NewServer(s.logger, s.operatorPrivKey, s.remoteSigner, s.password)
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
		s.remoteSigner.listKeysError = errors.New("remote signer error")
		resp, err := s.ServeHTTP("GET", pathValidators, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.listKeysError = nil
	})
}

func (s *ServerTestSuite) TestAddValidator() {
	t := s.T()

	sk := new(bls.SecretKey)
	sk.SetByCSPRNG()
	pubKey := sk.GetPublicKey().Serialize()

	validBlsKey := "0x" + hex.EncodeToString(sk.Serialize())
	s.operatorPrivKey.decryptResult = []byte(validBlsKey)

	request := AddValidatorRequest{
		ShareKeys: []ShareKeys{
			{
				EncryptedPrivKey: []byte("encrypted_key"),
				PublicKey:        phase0.BLSPubKey(pubKey),
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
					PublicKey:        phase0.BLSPubKey{},
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
		s.operatorPrivKey.decryptResult = []byte{}

		invalidPrivKeyRequest := AddValidatorRequest{
			ShareKeys: []ShareKeys{
				{
					EncryptedPrivKey: []byte{},
					PublicKey:        phase0.BLSPubKey(pubKey),
				},
			},
		}
		invalidPrivKeyReqBody, err := json.Marshal(invalidPrivKeyRequest)
		require.NoError(t, err)
		resp, err := s.ServeHTTP("POST", pathValidators, invalidPrivKeyReqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.decryptResult = []byte(validBlsKey)
	})

	t.Run("decryption error", func(t *testing.T) {
		s.operatorPrivKey.decryptError = errors.New("decryption error")
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.decryptError = nil
	})

	t.Run("invalid decrypt result", func(t *testing.T) {
		s.operatorPrivKey.decryptResult = []byte("not-a-hex-string")
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.decryptResult = []byte(validBlsKey)
	})

	t.Run("different decrypt result", func(t *testing.T) {
		differentSk := new(bls.SecretKey)
		differentSk.SetByCSPRNG()
		differentBlsKey := "0x" + hex.EncodeToString(differentSk.Serialize())
		s.operatorPrivKey.decryptResult = []byte(differentBlsKey)

		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

		s.operatorPrivKey.decryptResult = []byte(validBlsKey)
	})

	t.Run("import error", func(t *testing.T) {
		s.remoteSigner.importError = errors.New("import error")
		resp, err := s.ServeHTTP("POST", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.importError = nil
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
		s.remoteSigner.deleteResult = web3signer.DeleteKeystoreResponse{Data: []web3signer.KeyManagerResponseData{{Status: web3signer.StatusDeleted}, {Status: web3signer.StatusNotFound}}}
		resp, err := s.ServeHTTP("DELETE", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	})

	t.Run("custom delete error", func(t *testing.T) {
		s.remoteSigner.deleteResult = web3signer.DeleteKeystoreResponse{Data: []web3signer.KeyManagerResponseData{{Status: web3signer.StatusDeleted}}}

		s.remoteSigner.deleteError = errors.New("remote signer error")
		resp, err := s.ServeHTTP("DELETE", pathValidators, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.deleteError = nil
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
		s.remoteSigner.signError = errors.New("remote signer error")
		resp, err := s.ServeHTTP("POST", pathValidatorsSign+pubKeyHex, reqBody)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.remoteSigner.signError = nil
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
		s.pubKey.base64Error = errors.New("base64 error")
		resp, err := s.ServeHTTP("GET", pathOperatorIdentity, nil)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.pubKey.base64Error = nil
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
		s.operatorPrivKey.signError = errors.New("sign error")
		resp, err := s.ServeHTTP("POST", pathOperatorSign, messageToSign)
		require.NoError(t, err)
		assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

		s.operatorPrivKey.signError = nil
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

type testOperatorPublicKey struct {
	pubKeyBase64 string
	base64Error  error
}

func (t *testOperatorPublicKey) Encrypt(data []byte) ([]byte, error) {
	return data, nil
}

func (t *testOperatorPublicKey) Verify(data []byte, signature []byte) error {
	return nil
}

func (t *testOperatorPublicKey) Base64() (string, error) {
	return t.pubKeyBase64, t.base64Error
}

type testOperatorPrivateKey struct {
	base64Value   string
	bytesValue    []byte
	storageHash   string
	ekmHash       string
	decryptResult []byte
	decryptError  error
	signResult    []byte
	signError     error
	publicKey     keys.OperatorPublicKey
}

func (t *testOperatorPrivateKey) Sign(data []byte) ([]byte, error) {
	if t.signError != nil {
		return nil, t.signError
	}
	return t.signResult, nil
}

func (t *testOperatorPrivateKey) Public() keys.OperatorPublicKey {
	return t.publicKey
}

func (t *testOperatorPrivateKey) Decrypt(encryptedData []byte) ([]byte, error) {
	if t.decryptError != nil {
		return nil, t.decryptError
	}
	return t.decryptResult, nil
}

func (t *testOperatorPrivateKey) StorageHash() string {
	return t.storageHash
}

func (t *testOperatorPrivateKey) EKMHash() string {
	return t.ekmHash
}

func (t *testOperatorPrivateKey) Bytes() []byte {
	return t.bytesValue
}

func (t *testOperatorPrivateKey) Base64() string {
	return t.base64Value
}

type testRemoteSigner struct {
	listKeysResult []phase0.BLSPubKey
	listKeysError  error
	importResult   web3signer.ImportKeystoreResponse
	importError    error
	deleteResult   web3signer.DeleteKeystoreResponse
	deleteError    error
	signResult     web3signer.SignResponse
	signError      error
}

func (t *testRemoteSigner) ListKeys(ctx context.Context) (web3signer.ListKeysResponse, error) {
	if t.listKeysError != nil {
		return nil, t.listKeysError
	}
	return t.listKeysResult, nil
}

func (t *testRemoteSigner) ImportKeystore(ctx context.Context, req web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error) {
	if t.importError != nil {
		return web3signer.ImportKeystoreResponse{}, t.importError
	}
	return t.importResult, nil
}

func (t *testRemoteSigner) DeleteKeystore(ctx context.Context, req web3signer.DeleteKeystoreRequest) (web3signer.DeleteKeystoreResponse, error) {
	if t.deleteError != nil {
		return web3signer.DeleteKeystoreResponse{}, t.deleteError
	}
	return t.deleteResult, nil
}

func (t *testRemoteSigner) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, req web3signer.SignRequest) (web3signer.SignResponse, error) {
	if t.signError != nil {
		return web3signer.SignResponse{}, t.signError
	}
	return t.signResult, nil
}
