package ssvsigner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/keys"

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
	require.NoError(s.T(), err)

	err = bls.Init(bls.BLS12_381)
	require.NoError(s.T(), err)

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
		importResult:   []web3signer.Status{web3signer.StatusImported},
		deleteResult:   []web3signer.Status{web3signer.StatusDeleted},
		signResult:     []byte("signature_bytes"),
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

	resp, err := s.ServeHTTP("GET", "/v1/validators/list", nil)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	var response []string
	err = json.Unmarshal(resp.Body(), &response)
	require.NoError(t, err)
	assert.Equal(t, []string{"0x123", "0x456"}, response)

	s.remoteSigner.listKeysError = errors.New("remote signer error")
	resp, err = s.ServeHTTP("GET", "/v1/validators/list", nil)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.remoteSigner.listKeysError = nil
}

func (s *ServerTestSuite) TestAddValidator() {
	t := s.T()

	sk := new(bls.SecretKey)
	sk.SetByCSPRNG()
	pubKey := sk.GetPublicKey().Serialize()

	validBlsKey := fmt.Sprintf("0x%s", hex.EncodeToString(sk.Serialize()))
	s.operatorPrivKey.decryptResult = []byte(validBlsKey)

	request := AddValidatorRequest{
		ShareKeys: []ServerShareKeys{
			{
				EncryptedPrivKey: hex.EncodeToString([]byte("encrypted_key")),
				PublicKey:        hex.EncodeToString(pubKey),
			},
		},
	}
	reqBody, err := json.Marshal(request)
	require.NoError(t, err)

	resp, err := s.ServeHTTP("POST", "/v1/validators/add", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	var response AddValidatorResponse
	err = json.Unmarshal(resp.Body(), &response)
	require.NoError(t, err)
	assert.Equal(t, []web3signer.Status{web3signer.StatusImported}, response.Statuses)

	resp, err = s.ServeHTTP("POST", "/v1/validators/add", []byte("{invalid json}"))
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())

	emptyRequest := AddValidatorRequest{
		ShareKeys: []ServerShareKeys{},
	}
	emptyReqBody, err := json.Marshal(emptyRequest)
	require.NoError(t, err)
	resp, err = s.ServeHTTP("POST", "/v1/validators/add", emptyReqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	invalidPubKeyRequest := AddValidatorRequest{
		ShareKeys: []ServerShareKeys{
			{
				EncryptedPrivKey: hex.EncodeToString([]byte("encrypted_key")),
				PublicKey:        "invalid_hex",
			},
		},
	}
	invalidPubKeyReqBody, err := json.Marshal(invalidPubKeyRequest)
	require.NoError(t, err)
	resp, err = s.ServeHTTP("POST", "/v1/validators/add", invalidPubKeyReqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())

	invalidPrivKeyRequest := AddValidatorRequest{
		ShareKeys: []ServerShareKeys{
			{
				EncryptedPrivKey: "invalid_hex",
				PublicKey:        hex.EncodeToString(pubKey),
			},
		},
	}
	invalidPrivKeyReqBody, err := json.Marshal(invalidPrivKeyRequest)
	require.NoError(t, err)
	resp, err = s.ServeHTTP("POST", "/v1/validators/add", invalidPrivKeyReqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())

	s.operatorPrivKey.decryptError = errors.New("decryption error")
	resp, err = s.ServeHTTP("POST", "/v1/validators/add", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

	s.operatorPrivKey.decryptError = nil

	s.operatorPrivKey.decryptResult = []byte("not-a-hex-string")
	resp, err = s.ServeHTTP("POST", "/v1/validators/add", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.operatorPrivKey.decryptResult = []byte(validBlsKey)

	differentSk := new(bls.SecretKey)
	differentSk.SetByCSPRNG()
	differentBlsKey := fmt.Sprintf("0x%s", hex.EncodeToString(differentSk.Serialize()))
	s.operatorPrivKey.decryptResult = []byte(differentBlsKey)

	resp, err = s.ServeHTTP("POST", "/v1/validators/add", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusUnprocessableEntity, resp.StatusCode())

	s.operatorPrivKey.decryptResult = []byte(validBlsKey)

	s.remoteSigner.importError = errors.New("import error")
	resp, err = s.ServeHTTP("POST", "/v1/validators/add", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.remoteSigner.importError = nil
}

func (s *ServerTestSuite) TestRemoveValidator() {
	t := s.T()

	request := RemoveValidatorRequest{
		PublicKeys: []string{"0x123", "0x456"},
	}
	reqBody, err := json.Marshal(request)
	require.NoError(t, err)

	resp, err := s.ServeHTTP("POST", "/v1/validators/remove", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	var response RemoveValidatorResponse
	err = json.Unmarshal(resp.Body(), &response)
	require.NoError(t, err)
	assert.Equal(t, []web3signer.Status{web3signer.StatusDeleted}, response.Statuses)

	resp, err = s.ServeHTTP("POST", "/v1/validators/remove", []byte("{invalid json}"))
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())

	emptyRequest := RemoveValidatorRequest{
		PublicKeys: []string{},
	}
	emptyReqBody, err := json.Marshal(emptyRequest)
	require.NoError(t, err)
	resp, err = s.ServeHTTP("POST", "/v1/validators/remove", emptyReqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	s.remoteSigner.deleteResult = []web3signer.Status{web3signer.StatusDeleted, web3signer.StatusNotFound}
	resp, err = s.ServeHTTP("POST", "/v1/validators/remove", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())

	s.remoteSigner.deleteResult = []web3signer.Status{web3signer.StatusDeleted}

	s.remoteSigner.deleteError = errors.New("remote signer error")
	resp, err = s.ServeHTTP("POST", "/v1/validators/remove", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.remoteSigner.deleteError = nil
}

func (s *ServerTestSuite) TestSignValidator() {
	t := s.T()

	pubKeyHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	signPayload := web3signer.SignRequest{
		Type: web3signer.TypeAttestation,
	}

	reqBody, err := json.Marshal(signPayload)
	require.NoError(t, err)

	resp, err := s.ServeHTTP("POST", fmt.Sprintf("/v1/validators/sign/%s", pubKeyHex), reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	assert.Equal(t, []byte("signature_bytes"), resp.Body())

	resp, err = s.ServeHTTP("POST", fmt.Sprintf("/v1/validators/sign/%s", pubKeyHex), []byte("{invalid json}"))
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())

	resp, err = s.ServeHTTP("POST", "/v1/validators/sign/", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusNotFound, resp.StatusCode())

	resp, err = s.ServeHTTP("POST", "/v1/validators/sign/invalid", reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusBadRequest, resp.StatusCode())

	s.remoteSigner.signError = errors.New("remote signer error")
	resp, err = s.ServeHTTP("POST", fmt.Sprintf("/v1/validators/sign/%s", pubKeyHex), reqBody)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.remoteSigner.signError = nil
}

func (s *ServerTestSuite) TestOperatorIdentity() {
	t := s.T()

	resp, err := s.ServeHTTP("GET", "/v1/operator/identity", nil)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	assert.Equal(t, "test_pubkey_base64", string(resp.Body()))

	s.pubKey.base64Error = errors.New("base64 error")
	resp, err = s.ServeHTTP("GET", "/v1/operator/identity", nil)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.pubKey.base64Error = nil
}

func (s *ServerTestSuite) TestOperatorSign() {
	t := s.T()

	messageToSign := []byte("message_to_sign")

	resp, err := s.ServeHTTP("POST", "/v1/operator/sign", messageToSign)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusOK, resp.StatusCode())
	assert.Equal(t, []byte("signature_bytes"), resp.Body())

	s.operatorPrivKey.signError = errors.New("sign error")
	resp, err = s.ServeHTTP("POST", "/v1/operator/sign", messageToSign)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusInternalServerError, resp.StatusCode())

	s.operatorPrivKey.signError = nil
}

func (s *ServerTestSuite) TestRouting() {
	t := s.T()

	resp, err := s.ServeHTTP("GET", "/non-existent-route", nil)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusNotFound, resp.StatusCode())

	resp, err = s.ServeHTTP("PUT", "/v1/validators/list", nil)
	require.NoError(t, err)
	assert.Equal(t, fasthttp.StatusMethodNotAllowed, resp.StatusCode())
}

func (s *ServerTestSuite) TestHelperMethods() {
	t := s.T()

	ctx := &fasthttp.RequestCtx{}

	s.server.writeString(ctx, "test string")
	assert.Equal(t, "test string", string(ctx.Response.Body()))

	ctx.Response.Reset()
	s.server.writeBytes(ctx, []byte("test bytes"))
	assert.Equal(t, "test bytes", string(ctx.Response.Body()))

	ctx.Response.Reset()
	s.server.writeErr(ctx, errors.New("test error"))
	assert.Equal(t, "test error", string(ctx.Response.Body()))

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

func (t *testOperatorPrivateKey) StorageHash() (string, error) {
	return t.storageHash, nil
}

func (t *testOperatorPrivateKey) EKMHash() (string, error) {
	return t.ekmHash, nil
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
	importResult   []web3signer.Status
	importError    error
	deleteResult   []web3signer.Status
	deleteError    error
	signResult     phase0.BLSSignature
	signError      error
}

func (t *testRemoteSigner) ListKeys(ctx context.Context) ([]phase0.BLSPubKey, error) {
	if t.listKeysError != nil {
		return nil, t.listKeysError
	}
	return t.listKeysResult, nil
}

func (t *testRemoteSigner) ImportKeystore(ctx context.Context, keystoreList []web3signer.Keystore, keystorePasswordList []string) ([]web3signer.Status, error) {
	if t.importError != nil {
		return nil, t.importError
	}
	return t.importResult, nil
}

func (t *testRemoteSigner) DeleteKeystore(ctx context.Context, sharePubKeyList []phase0.BLSPubKey) ([]web3signer.Status, error) {
	if t.deleteError != nil {
		return nil, t.deleteError
	}
	return t.deleteResult, nil
}

func (t *testRemoteSigner) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error) {
	if t.signError != nil {
		return phase0.BLSSignature{}, t.signError
	}
	return t.signResult, nil
}
