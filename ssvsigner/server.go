package ssvsigner

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/fasthttp/router"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// The routes are custom, per DESIGN.md, and differ from the web3signer
// (https://consensys.github.io/web3signer/web3signer-eth2.html) and eth remote-signing
// (https://github.com/ethereum/remote-signing-api) APIs, which prefix theirs with /api/v1/.
// Deployed nodes and signers depend on these paths, so renaming them would be a breaking change.
const (
	PathValidators       = "/v1/validators"
	PathValidatorsSign   = "/v1/validators/sign/"
	PathOperatorIdentity = "/v1/operator/identity"
	PathOperatorSign     = "/v1/operator/sign"
	PathOperatorEncrypt  = "/v1/operator/encrypt"
	PathOperatorDecrypt  = "/v1/operator/decrypt"
)

const (
	// Processing one share takes ~0.5-0.8s, so 10 shares seem a reasonable limit.
	addShareLimit = 10
)

type Server struct {
	logger          *zap.Logger
	operatorPrivKey keys.OperatorPrivateKey
	remoteSigner    web3signer.RemoteSigner
	router          *router.Router
	tlsConfig       *tls.Config
}

func NewServer(
	logger *zap.Logger,
	operatorPrivKey keys.OperatorPrivateKey,
	remoteSigner web3signer.RemoteSigner,
	opts ...Option,
) *Server {
	r := router.New()

	server := &Server{
		logger:          logger,
		operatorPrivKey: operatorPrivKey,
		remoteSigner:    remoteSigner,
		router:          r,
	}

	for _, opt := range opts {
		opt(server)
	}

	r.GET(PathValidators, server.handleListValidators)
	r.POST(PathValidators, server.handleAddValidator)
	r.DELETE(PathValidators, server.handleRemoveValidator)
	r.POST(PathValidatorsSign+"{identifier}", server.handleSignValidator)

	r.GET(PathOperatorIdentity, server.handleOperatorIdentity)
	r.POST(PathOperatorSign, server.handleOperatorSign)
	r.POST(PathOperatorEncrypt, server.handleOperatorEncrypt)
	r.POST(PathOperatorDecrypt, server.handleOperatorDecrypt)

	return server
}

type Option func(*Server)

// WithTLS configures TLS for the server.
//
// This method takes a pre-configured TLS config object that defines the server's
// TLS certificate and optional client authentication settings.
//
// Parameters:
//   - tlsConfig: A complete tls.Config object, typically created by tls.LoadServerTLSConfig()
func WithTLS(tlsConfig *tls.Config) func(*Server) {
	return func(s *Server) {
		s.tlsConfig = tlsConfig
	}
}

func (s *Server) Handler() func(ctx *fasthttp.RequestCtx) {
	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("request panicked", zap.Any("panic", r))
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetBodyString("Internal server error")
			}

			route := string(ctx.Path())
			matchedRoute := ctx.UserValue(router.MatchedRoutePathParam)
			if matchedRouteStr, ok := matchedRoute.(string); ok {
				route = matchedRouteStr
			}
			recordHTTPRequest(
				ctx, // Use fasthttp context directly
				route,
				string(ctx.Method()),
				ctx.Response.StatusCode(),
				time.Since(start),
			)
		}()
		s.router.Handler(ctx)
	}
}

// ListenAndServe starts the server on the specified address.
// If TLS is configured, it will use HTTPS.
func (s *Server) ListenAndServe(addr string) error {
	handler := s.Handler()

	if s.tlsConfig != nil {
		s.logger.Info("starting server with TLS", zap.String("addr", addr))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}

		tlsLn := tls.NewListener(ln, s.tlsConfig)
		return fasthttp.Serve(tlsLn, handler)
	}

	s.logger.Info("starting server without TLS", zap.String("addr", addr))
	return fasthttp.ListenAndServe(addr, handler)
}

func (s *Server) handleListValidators(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(zap.String("method", "handleListValidators"))
	logger.Debug("received request")

	start := time.Now()
	resp, err := s.remoteSigner.ListKeys(ctx)
	recordRemoteSignerOperation(ctx, opRemoteSignerListKeys, err, time.Since(start))
	logger = logger.With(zap.Duration("took", time.Since(start)))
	if err != nil {
		s.handleWeb3SignerErr(ctx, logger, err)
		return
	}

	logger.Info("request finished successfully", zap.Int("count", len(resp)))
	s.writeJSON(ctx, logger, resp)
}

func (s *Server) handleAddValidator(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(zap.String("method", "handleAddValidator"))
	logger.Debug("received request")

	var req AddValidatorRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		logger.Warn("failed to unmarshal request body", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	logger = logger.With(zap.Int("req_count", len(req.ShareKeys)))

	if len(req.ShareKeys) > addShareLimit {
		logger.Warn("requested too many shares to be added")
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest,
			fmt.Errorf("requested too many shares to be added: %d", len(req.ShareKeys)))
		return
	}

	var importKeystoreReq web3signer.ImportKeystoreRequest
	for i, share := range req.ShareKeys {
		logger := logger.With(zap.Stringer("share_pubkey", share.PubKey))

		// The password is used to encrypt a keystore and to decrypt and save it in web3signer afterwards.
		// So, there's no need to store the password. We can just generate a random password for each keystore.
		keystorePassword, err := s.generateRandomPassword(16)
		if err != nil {
			// Internal (transient) failure, not a bad share -> 500 (see errMalformedShare). Log at
			// error to match the node-fatal reply.
			logger.Error("failed to generate random password", zap.Error(err))
			s.writeJSONErr(
				ctx,
				logger,
				fasthttp.StatusInternalServerError,
				fmt.Errorf("failed to generate random password: %w", err),
			)
			return
		}

		keystoreJSON, err := s.keystoreJSONFromEncryptedShare(
			share.EncryptedPrivKey,
			share.PubKey,
			keystorePassword,
		)
		if err != nil {
			// 422 only for a malformed share; anything else defaults to 500 (see errMalformedShare).
			// Log level follows the reply: warn for the skippable 422, error for a node-fatal 500.
			status := fasthttp.StatusInternalServerError
			logAt := logger.Error
			if errors.Is(err, errMalformedShare) {
				status = fasthttp.StatusUnprocessableEntity
				logAt = logger.Warn
			}
			logAt("failed to get keystore from encrypted share", zap.Error(err))
			s.writeJSONErr(
				ctx,
				logger,
				status,
				fmt.Errorf("failed to get keystore from encrypted share index %d: %w", i, err),
			)
			return
		}

		importKeystoreReq.Keystores = append(importKeystoreReq.Keystores, keystoreJSON)
		importKeystoreReq.Passwords = append(importKeystoreReq.Passwords, keystorePassword)
	}

	start := time.Now()
	resp, err := s.remoteSigner.ImportKeystore(ctx, importKeystoreReq)
	recordRemoteSignerOperation(ctx, opRemoteSignerImportKeystore, err, time.Since(start))
	logger = logger.With(zap.Duration("took", time.Since(start)))
	if err != nil {
		s.handleImportKeystoreErr(ctx, logger, err)
		return
	}

	logger = logger.With(zap.Int("resp_count", len(resp.Data)))

	var importedCount int
	for i, data := range resp.Data {
		if data.Status != web3signer.StatusImported {
			logger.Warn("unexpected keystore status",
				zap.String("status", string(data.Status)),
				zap.String("message", data.Message),
				zap.Stringer("share_pubkey", req.ShareKeys[i].PubKey),
			)
		} else {
			importedCount++
		}
	}

	logger.Info("request finished successfully", zap.Int("imported_count", importedCount))
	s.writeJSON(ctx, logger, resp)
}

// errMalformedShare tags the keystoreJSONFromEncryptedShare failures caused by a bad share
// (undecryptable, bad hex, invalid BLS key, or pubkey mismatch). handleAddValidator replies 422
// for these — which the node skips as a malformed event — and 500 for everything else. Making 422
// opt-in keeps the "silently skip" path explicit: an untagged internal failure crash-retries
// instead of dropping a valid validator.
var errMalformedShare = errors.New("malformed share")

// keystoreJSONFromEncryptedShare withholds the cause of share-data failures (tagged
// errMalformedShare) so a bad share can't leak private-key material. Failures after the share
// validates are internal, carry no key bytes, and pass their cause through for diagnosis.
func (s *Server) keystoreJSONFromEncryptedShare(
	encryptedPrivKey hexutil.Bytes,
	sharePubKey phase0.BLSPubKey,
	keystorePassword string,
) (string, error) {
	sharePrivKeyHex, err := s.operatorPrivKey.Decrypt(encryptedPrivKey)
	if err != nil {
		return "", fmt.Errorf("%w: decrypt share", errMalformedShare)
	}

	sharePrivKey, err := hex.DecodeString(strings.TrimPrefix(string(sharePrivKeyHex), "0x"))
	if err != nil {
		return "", fmt.Errorf("%w: decode share private key from hex for pubkey %s", errMalformedShare, sharePubKey.String())
	}

	sharePrivBLS := &bls.SecretKey{}
	if err = sharePrivBLS.Deserialize(sharePrivKey); err != nil {
		return "", fmt.Errorf("%w: deserialize share private key", errMalformedShare)
	}

	if !bytes.Equal(sharePrivBLS.GetPublicKey().Serialize(), sharePubKey[:]) {
		return "", fmt.Errorf("%w: derived public key does not match expected public key", errMalformedShare)
	}

	// Past this point the share is valid; remaining failures are internal (untagged -> 500).
	shareKeystore, err := keystore.GenerateShareKeystore(sharePrivBLS, sharePubKey, keystorePassword)
	if err != nil {
		return "", fmt.Errorf("generate share keystore: %w", err)
	}

	keystoreJSON, err := json.Marshal(shareKeystore)
	if err != nil {
		return "", fmt.Errorf("marshal share keystore: %w", err)
	}

	return string(keystoreJSON), nil
}

func (s *Server) generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	password := make([]byte, length)
	for i := range password {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		password[i] = charset[idx.Int64()]
	}
	return string(password), nil
}

func (s *Server) handleRemoveValidator(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(zap.String("method", "handleRemoveValidator"))
	logger.Debug("received request")

	var req web3signer.DeleteKeystoreRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		logger.Warn("failed to unmarshal request body", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	logger = logger.With(zap.Int("req_count", len(req.Pubkeys)))

	start := time.Now()
	resp, err := s.remoteSigner.DeleteKeystore(ctx, req)
	recordRemoteSignerOperation(ctx, opRemoteSignerDeleteKeystore, err, time.Since(start))
	logger = logger.With(zap.Duration("took", time.Since(start)))
	if err != nil {
		s.handleWeb3SignerErr(ctx, logger, err)
		return
	}

	logger = logger.With(zap.Int("resp_count", len(resp.Data)))

	var deletedCount int
	for i, data := range resp.Data {
		if data.Status != web3signer.StatusDeleted {
			logger.Warn("unexpected keystore status",
				zap.String("status", string(data.Status)),
				zap.String("message", data.Message),
				zap.Stringer("share_pubkey", req.Pubkeys[i]),
			)
		} else {
			deletedCount++
		}
	}

	logger.Info("request finished successfully", zap.Int("deleted_count", deletedCount))
	s.writeJSON(ctx, logger, resp)
}

func (s *Server) handleSignValidator(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(zap.String("method", "handleSignValidator"))
	logger.Debug("received request")

	identifierValue := ctx.UserValue("identifier")
	blsPubKey, err := s.extractShareKey(identifierValue)
	if err != nil {
		logger.Warn("failed to extract share key", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("extract share key: %w", err))
		return
	}

	logger = logger.With(zap.Stringer("share_pubkey", blsPubKey))

	var req web3signer.SignRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		logger.Warn("failed to unmarshal request body", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("unmarshal request body: %w", err))
		return
	}

	logger = logger.With(zap.String("type", string(req.Type)))

	start := time.Now()
	resp, err := s.remoteSigner.Sign(ctx, blsPubKey, req)
	recordRemoteSignerOperation(ctx, opRemoteSignerValidatorSign, err, time.Since(start))
	logger = logger.With(zap.Duration("took", time.Since(start)))
	if err != nil {
		s.handleWeb3SignerErr(ctx, logger, err)
		return
	}

	logger.Info("request finished successfully")
	s.writeJSON(ctx, logger, resp)
}

func (s *Server) extractShareKey(identifierValue any) (phase0.BLSPubKey, error) {
	sharePubKeyHex, ok := identifierValue.(string)
	if !ok {
		return phase0.BLSPubKey{}, fmt.Errorf("unexpected share public key type %T", identifierValue)
	}

	sharePubKey, err := hex.DecodeString(strings.TrimPrefix(sharePubKeyHex, "0x"))
	if err != nil {
		return phase0.BLSPubKey{}, fmt.Errorf("decode share public key hex: %w", err)
	}

	if len(sharePubKey) != len(phase0.BLSPubKey{}) {
		return phase0.BLSPubKey{}, fmt.Errorf("invalid share public key length %d, expected %d", len(sharePubKey), len(phase0.BLSPubKey{}))
	}

	return phase0.BLSPubKey(sharePubKey), nil
}

func (s *Server) handleOperatorIdentity(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(zap.String("method", "handleOperatorIdentity"))
	logger.Debug("received request")

	pubKeyB64, err := s.operatorPrivKey.Public().Base64()
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	logger.Info("request finished successfully")
	ctx.SetStatusCode(fasthttp.StatusOK)
	s.writeString(ctx, logger, pubKeyB64)
}

func (s *Server) handleOperatorSign(ctx *fasthttp.RequestCtx) {
	payload := ctx.PostBody()

	logger := s.logger.With(
		zap.String("method", "handleOperatorSign"),
		zap.Int("payload_size", len(payload)),
	)

	logger.Debug("received request")

	if len(payload) == 0 {
		logger.Warn("request has no payload")
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, errors.New("request payload is empty"))
		return
	}

	signature, err := s.operatorPrivKey.Sign(payload)
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	logger.Info("request finished successfully")
	ctx.SetStatusCode(fasthttp.StatusOK)
	s.writeBytes(ctx, logger, signature)
}

func (s *Server) handleOperatorEncrypt(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(
		zap.String("method", "handleOperatorEncrypt"),
		zap.Int("payload_size", len(ctx.PostBody())),
	)

	logger.Debug("received request")

	payload := ctx.PostBody()
	if len(payload) == 0 {
		err := errors.New("request payload is empty")
		logger.Warn("invalid request", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, err)
		return
	}

	encryptionKey, err := s.operatorPrivKey.EKMEncryptionKey()
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	encrypted, err := keys.EncryptPayload(encryptionKey, payload)
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	logger.Info("request finished successfully")
	ctx.SetStatusCode(fasthttp.StatusOK)
	s.writeBytes(ctx, logger, encrypted)
}

func (s *Server) handleOperatorDecrypt(ctx *fasthttp.RequestCtx) {
	logger := s.logger.With(
		zap.String("method", "handleOperatorDecrypt"),
		zap.Int("payload_size", len(ctx.PostBody())),
	)

	logger.Debug("received request")

	payload := ctx.PostBody()
	if len(payload) == 0 {
		err := errors.New("request payload is empty")
		logger.Warn("invalid request", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, err)
		return
	}

	encryptionKey, err := s.operatorPrivKey.EKMEncryptionKey()
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	decrypted, err := keys.DecryptPayload(encryptionKey, payload)
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		s.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}
	logger.Info("request finished successfully")
	ctx.SetStatusCode(fasthttp.StatusOK)
	s.writeBytes(ctx, logger, decrypted)
}

// web3SignerErrStatus returns the HTTP status to surface for a failed Web3Signer request:
// the upstream status when known, or 500 for transport failures (connection errors, timeouts).
func web3SignerErrStatus(err error) int {
	var he web3signer.HTTPResponseError
	if errors.As(err, &he) {
		return he.Status
	}
	return fasthttp.StatusInternalServerError
}

// handleWeb3SignerErr responds with the Web3Signer failure's status and an error body
// describing the reason, so clients can log the actual cause instead of a bare status code.
func (s *Server) handleWeb3SignerErr(ctx *fasthttp.RequestCtx, logger *zap.Logger, err error) {
	statusCode := web3SignerErrStatus(err)
	logger.Error("web3signer request failed",
		zap.Error(err),
		zap.Int("status_code", statusCode),
	)
	s.writeJSONErr(ctx, logger, statusCode, err)
}

// handleImportKeystoreErr handles a failed keystore import. Unlike handleWeb3SignerErr it never
// relays the upstream error body: the import request carries the share keystore JSON and its
// plaintext password, so a server that echoes the request back in an error could leak recoverable
// key material to the node — the reason keystoreJSONFromEncryptedShare also withholds its errors.
// The upstream status is still forwarded, except a 422, which is remapped to 502 so it can't be
// read as ssv-signer's own share-decryption failure (see Client.AddValidators). The full error is
// still logged locally.
func (s *Server) handleImportKeystoreErr(ctx *fasthttp.RequestCtx, logger *zap.Logger, err error) {
	statusCode := web3SignerErrStatus(err)
	if statusCode == fasthttp.StatusUnprocessableEntity {
		statusCode = fasthttp.StatusBadGateway
	}
	logger.Error("web3signer request failed",
		zap.Error(err),
		zap.Int("status_code", statusCode),
	)
	s.writeJSONErr(ctx, logger, statusCode, errors.New("keystore import failed"))
}

func (s *Server) writeString(ctx *fasthttp.RequestCtx, logger *zap.Logger, str string) {
	if _, err := ctx.WriteString(str); err != nil {
		logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (s *Server) writeBytes(ctx *fasthttp.RequestCtx, logger *zap.Logger, b []byte) {
	if _, err := ctx.Write(b); err != nil {
		logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (s *Server) writeJSON(ctx *fasthttp.RequestCtx, logger *zap.Logger, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		logger.Error("failed to marshal JSON", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		errResp := web3signer.ErrorMessage{Message: err.Error()}
		b, err = json.Marshal(errResp)
		if err != nil {
			logger.Error("failed to marshal JSON error", zap.Error(err))
			s.writeString(ctx, logger, fmt.Sprintf("failed to marshal JSON error: %v", err))
			return
		}
	}

	ctx.SetContentType("application/json")
	if _, err := ctx.Write(b); err != nil {
		logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

// writeJSONErr calls writeJSON, so it shouldn't be called from writeJSON
func (s *Server) writeJSONErr(ctx *fasthttp.RequestCtx, logger *zap.Logger, statusCode int, err error) {
	ctx.SetStatusCode(statusCode)
	errResp := web3signer.ErrorMessage{Message: err.Error()}
	s.writeJSON(ctx, logger, errResp)
}
