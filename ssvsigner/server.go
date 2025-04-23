package ssvsigner

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/fasthttp/router"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

const (
	// TODO: The routes are currently custom and adhere DESIGN.md.
	//  However, web3signer and eth remote signer APIs use different ones (prefixed with /api/v1/):
	//  - https://consensys.github.io/web3signer/web3signer-eth2.html
	//  - https://github.com/ethereum/remote-signing-api
	//  We need to decide if we need to match them.
	pathValidators       = "/v1/validators"        // TODO: /api/v1/eth2/publicKeys ?
	pathValidatorsSign   = "/v1/validators/sign/"  // TODO: /api/v1/eth2/sign/ ?
	pathOperatorIdentity = "/v1/operator/identity" // TODO: /api/v1/ssv/identity ?
	pathOperatorSign     = "/v1/operator/sign"     // TODO: /api/v1/ssv/sign ?
)

type Server struct {
	logger          *zap.Logger
	operatorPrivKey keys.OperatorPrivateKey
	remoteSigner    remoteSigner
	router          *router.Router
}

func NewServer(
	logger *zap.Logger,
	operatorPrivKey keys.OperatorPrivateKey,
	remoteSigner remoteSigner,
) *Server {
	r := router.New()

	server := &Server{
		logger:          logger,
		operatorPrivKey: operatorPrivKey,
		remoteSigner:    remoteSigner,
		router:          r,
	}

	r.GET(pathValidators, server.handleListValidators)
	r.POST(pathValidators, server.handleAddValidator)
	r.DELETE(pathValidators, server.handleRemoveValidator)
	r.POST(pathValidatorsSign+"{identifier}", server.handleSignValidator)

	r.GET(pathOperatorIdentity, server.handleOperatorIdentity)
	r.POST(pathOperatorSign, server.handleSignOperator)

	return server
}

func (r *Server) Handler() func(ctx *fasthttp.RequestCtx) {
	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()
		defer func() {
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
		r.router.Handler(ctx)
	}
}

func (r *Server) handleListValidators(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleListValidators"))
	logger.Debug("received request")

	start := time.Now()
	resp, err := r.remoteSigner.ListKeys(ctx)
	recordRemoteSignerOperation(ctx, opRemoteSignerListKeys, err, time.Since(start))

	if err != nil {
		r.handleWeb3SignerErr(ctx, logger, resp, err)
		return
	}

	logger.Info("request finished successfully", fields.Count(len(resp)))
	r.writeJSON(ctx, logger, resp)
}

func (r *Server) handleAddValidator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleAddValidator"))
	logger.Debug("received request")

	var req AddValidatorRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		logger.Warn("failed to unmarshal request body", zap.Error(err))
		r.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	logger = logger.With(zap.Int("req_count", len(req.ShareKeys)))

	var importKeystoreReq web3signer.ImportKeystoreRequest
	for i, share := range req.ShareKeys {
		logger := logger.With(zap.Stringer("share_pubkey", share.PubKey))

		// The password is used to encrypt a keystore and to decrypt and save it in web3signer afterwards.
		// So, there's no need to store the password. We can just generate a random password for each keystore.
		keystorePassword, err := r.generateRandomPassword(16)
		if err != nil {
			logger.Warn("failed to generate random password", zap.Error(err))
			r.writeJSONErr(
				ctx,
				logger,
				fasthttp.StatusUnprocessableEntity,
				fmt.Errorf("failed to generate random password: %w", err),
			)
			return
		}

		keystoreJSON, err := r.keystoreJSONFromEncryptedShare(
			share.EncryptedPrivKey,
			share.PubKey,
			keystorePassword,
		)
		if err != nil {
			logger.Warn("failed to get keystore from encrypted share", zap.Error(err))
			r.writeJSONErr(
				ctx,
				logger,
				fasthttp.StatusUnprocessableEntity,
				fmt.Errorf("failed to get keystore from encrypted share index %d: %w", i, err),
			)
			return
		}

		importKeystoreReq.Keystores = append(importKeystoreReq.Keystores, keystoreJSON)
		importKeystoreReq.Passwords = append(importKeystoreReq.Passwords, keystorePassword)
	}

	start := time.Now()
	resp, err := r.remoteSigner.ImportKeystore(ctx, importKeystoreReq)
	recordRemoteSignerOperation(ctx, opRemoteSignerImportKeystore, err, time.Since(start))
	if err != nil {
		r.handleWeb3SignerErr(ctx, logger, resp, err)
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
	r.writeJSON(ctx, logger, resp)
}

func (r *Server) keystoreJSONFromEncryptedShare(
	encryptedPrivKey hexutil.Bytes,
	sharePubKey phase0.BLSPubKey,
	keystorePassword string,
) (string, error) {
	sharePrivKeyHex, err := r.operatorPrivKey.Decrypt(encryptedPrivKey)
	if err != nil {
		return "", fmt.Errorf("decrypt share: %w", err)
	}

	sharePrivKey, err := hex.DecodeString(strings.TrimPrefix(string(sharePrivKeyHex), "0x"))
	if err != nil {
		return "", fmt.Errorf("decode share private key from hex %s: %w", string(sharePrivKeyHex), err)
	}

	sharePrivBLS := &bls.SecretKey{}
	if err = sharePrivBLS.Deserialize(sharePrivKey); err != nil {
		return "", fmt.Errorf("deserialize share private key: %w", err)
	}

	if !bytes.Equal(sharePrivBLS.GetPublicKey().Serialize(), sharePubKey[:]) {
		return "", errors.New("derived public key does not match expected public key")
	}

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

func (r *Server) generateRandomPassword(length int) (string, error) {
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

func (r *Server) handleRemoveValidator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleRemoveValidator"))
	logger.Debug("received request")

	var req web3signer.DeleteKeystoreRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		logger.Warn("failed to unmarshal request body", zap.Error(err))
		r.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	logger = logger.With(zap.Int("req_count", len(req.Pubkeys)))

	start := time.Now()
	resp, err := r.remoteSigner.DeleteKeystore(ctx, req)
	recordRemoteSignerOperation(ctx, opRemoteSignerDeleteKeystore, err, time.Since(start))
	if err != nil {
		r.handleWeb3SignerErr(ctx, logger, resp, err)
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
	r.writeJSON(ctx, logger, resp)
}

func (r *Server) handleSignValidator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleSignValidator"))
	logger.Debug("received request")

	identifierValue := ctx.UserValue("identifier")
	blsPubKey, err := r.extractShareKey(identifierValue)
	if err != nil {
		logger.Warn("failed to extract share key", zap.Error(err))
		r.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("extract share key: %w", err))
		return
	}

	logger = logger.With(fields.PubKey(blsPubKey[:]))

	var req web3signer.SignRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		logger.Warn("failed to unmarshal request body", zap.Error(err))
		r.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, fmt.Errorf("unmarshal request body: %w", err))
		return
	}

	logger = logger.With(zap.String("type", string(req.Type)))

	start := time.Now()
	resp, err := r.remoteSigner.Sign(ctx, blsPubKey, req)
	recordRemoteSignerOperation(ctx, opRemoteSignerValidatorSign, err, time.Since(start))
	if err != nil {
		logger = logger.With(zap.String("req", string(ctx.PostBody())))
		r.handleWeb3SignerErr(ctx, logger, resp, err)
		return
	}

	logger.Info("request finished successfully")
	r.writeJSON(ctx, logger, resp)
}

func (r *Server) extractShareKey(identifierValue any) (phase0.BLSPubKey, error) {
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

func (r *Server) handleOperatorIdentity(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleOperatorIdentity"))
	logger.Debug("received request")

	pubKeyB64, err := r.operatorPrivKey.Public().Base64()
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		r.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	logger.Info("request finished successfully")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeString(ctx, logger, pubKeyB64)
}

func (r *Server) handleSignOperator(ctx *fasthttp.RequestCtx) {
	payload := ctx.PostBody()

	logger := r.logger.With(
		zap.String("method", "handleSignOperator"),
		zap.Int("payload_size", len(payload)),
	)

	logger.Debug("received request")

	if len(payload) == 0 {
		logger.Warn("request has no payload")
		r.writeJSONErr(ctx, logger, fasthttp.StatusBadRequest, errors.New("request payload is empty"))
		return
	}

	signature, err := r.operatorPrivKey.Sign(payload)
	if err != nil {
		logger.Error("request failed", zap.Error(err))
		r.writeJSONErr(ctx, logger, fasthttp.StatusInternalServerError, err)
		return
	}

	logger.Info("request finished successfully")
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, logger, signature)
}

func (r *Server) handleWeb3SignerErr(ctx *fasthttp.RequestCtx, logger *zap.Logger, resp any, err error) {
	statusCode := fasthttp.StatusInternalServerError
	if he := new(web3signer.HTTPResponseError); errors.As(err, &he) {
		statusCode = he.Status
	}

	logger.Error("web3signer request failed",
		zap.Error(err),
		zap.Int("status_code", statusCode),
		zap.Any("resp", resp),
	)
	ctx.SetStatusCode(statusCode)
	r.writeJSON(ctx, logger, resp)
}

func (r *Server) writeString(ctx *fasthttp.RequestCtx, logger *zap.Logger, s string) {
	if _, err := ctx.WriteString(s); err != nil {
		logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (r *Server) writeBytes(ctx *fasthttp.RequestCtx, logger *zap.Logger, b []byte) {
	if _, err := ctx.Write(b); err != nil {
		logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (r *Server) writeJSON(ctx *fasthttp.RequestCtx, logger *zap.Logger, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		logger.Error("failed to marshal JSON", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		errResp := web3signer.ErrorMessage{Message: err.Error()}
		b, err = json.Marshal(errResp)
		if err != nil {
			logger.Error("failed to marshal JSON error", zap.Error(err))
			r.writeString(ctx, logger, fmt.Sprintf("failed to marshal JSON error: %v", err))
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
func (r *Server) writeJSONErr(ctx *fasthttp.RequestCtx, logger *zap.Logger, statusCode int, err error) {
	ctx.SetStatusCode(statusCode)
	errResp := web3signer.ErrorMessage{Message: err.Error()}
	r.writeJSON(ctx, logger, errResp)
}
