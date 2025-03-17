package ssvsigner

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/fasthttp/router"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type Server struct {
	logger          *zap.Logger
	operatorPrivKey keys.OperatorPrivateKey
	remoteSigner    remoteSigner
	router          *router.Router
	keystorePasswd  string
}

func NewServer(
	logger *zap.Logger,
	operatorPrivKey keys.OperatorPrivateKey,
	remoteSigner remoteSigner,
	keystorePasswd string,
) *Server {
	r := router.New()

	server := &Server{
		logger:          logger,
		operatorPrivKey: operatorPrivKey,
		remoteSigner:    remoteSigner,
		router:          r,
		keystorePasswd:  keystorePasswd,
	}

	r.GET("/v1/validators", server.handleListValidators)
	r.POST("/v1/validators", server.handleAddValidator)
	r.DELETE("/v1/validators", server.handleRemoveValidator)
	r.POST("/v1/validators/sign/{identifier}", server.handleSignValidator)

	r.GET("/v1/operator/identity", server.handleOperatorIdentity)
	r.POST("/v1/operator/sign", server.handleSignOperator)

	return server
}

func (r *Server) Handler() func(ctx *fasthttp.RequestCtx) {
	return r.router.Handler
}

func (r *Server) handleListValidators(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleListValidators"))

	publicKeys, err := r.remoteSigner.ListKeys(ctx)
	if err != nil {
		logger.Warn("Failed to request validators list from web3signer", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to import share to Web3Signer: %w", err))
		return
	}

	logger.Debug("Listed validators", fields.Count(len(publicKeys)))
	r.writeJSON(ctx, publicKeys)
}

func (r *Server) handleAddValidator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleAddValidator"))

	body := ctx.PostBody()
	if len(body) == 0 {
		logger.Warn("Request body is empty")
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeString(ctx, "request body is empty")
		return
	}

	var req AddValidatorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Warn("Failed to unmarshal request body", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	var shareKeystores []web3signer.Keystore
	var shareKeystorePasswords []string

	for i, share := range req.ShareKeys {
		logger := r.logger.With(zap.Int("index", i))
		encPrivKey, err := hex.DecodeString(strings.TrimPrefix(share.EncryptedPrivKey, "0x"))
		if err != nil {
			logger.Warn("Failed to decode share hex", zap.Error(err))
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			r.writeErr(ctx, fmt.Errorf("failed to decode share.EncryptedPrivKey as hex: %w", err))
			return
		}

		sharePrivKeyHex, err := r.operatorPrivKey.Decrypt(encPrivKey)
		if err != nil {
			logger.Warn("Failed to decrypt share", zap.Error(err))
			ctx.SetStatusCode(fasthttp.StatusUnprocessableEntity)
			r.writeErr(ctx, fmt.Errorf("failed to decrypt share: %w", err))
			return
		}

		sharePrivKey, err := hex.DecodeString(strings.TrimPrefix(string(sharePrivKeyHex), "0x"))
		if err != nil {
			logger.Warn("Failed to decode share private key hex", zap.Error(err))
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("failed to decode share private key from hex %s: %w", string(sharePrivKeyHex), err))
			return
		}

		sharePrivBLS := &bls.SecretKey{}
		if err = sharePrivBLS.Deserialize(sharePrivKey); err != nil {
			logger.Warn("Failed to deserialize share private key", zap.Error(err))
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("failed to deserialize share private key: %w", err))
			return
		}

		if !bytes.Equal(sharePrivBLS.GetPublicKey().Serialize(), share.PublicKey[:]) {
			logger.Warn("Derived public key does not match expected public key", zap.Int("index", i))
			ctx.SetStatusCode(fasthttp.StatusUnprocessableEntity)
			r.writeErr(ctx, fmt.Errorf("derived public key does not match expected public key"))
			return
		}

		shareKeystore, err := keystore.GenerateShareKeystore(sharePrivBLS, share.PublicKey, r.keystorePasswd)
		if err != nil {
			logger.Warn("Failed to generate share keystore", zap.Error(err))
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			r.writeErr(ctx, fmt.Errorf("failed to generate share keystore: %w", err))
			return
		}

		shareKeystores = append(shareKeystores, shareKeystore)
		shareKeystorePasswords = append(shareKeystorePasswords, r.keystorePasswd)
	}

	statuses, err := r.remoteSigner.ImportKeystore(ctx, shareKeystores, shareKeystorePasswords)
	if err != nil {
		logger.Warn("Failed to import keystores to web3signer", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to import keystores to Web3Signer: %w", err))
		return
	}

	resp := AddValidatorResponse{
		Statuses: statuses,
	}

	for i, status := range statuses {
		if status != web3signer.StatusImported {
			logger.Warn("Unexpected keystore status",
				zap.String("status", string(status)),
				zap.Stringer("share_pubkey", req.ShareKeys[i].PublicKey),
			)
		}
	}

	logger.Debug("Added validators", fields.Count(len(shareKeystores)))
	r.writeJSON(ctx, resp)
}

func (r *Server) handleRemoveValidator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleRemoveValidator"))

	body := ctx.PostBody()
	if len(body) == 0 {
		logger.Warn("Request body is empty")
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeString(ctx, "request body is empty")
		return
	}

	var req RemoveValidatorRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Warn("Failed to unmarshal request body", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	statuses, err := r.remoteSigner.DeleteKeystore(ctx, req.PublicKeys)
	if err != nil {
		logger.Warn("Failed to delete keystores from web3signer", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to remove keystores from Web3Signer: %w", err))
		return
	}

	for i, status := range statuses {
		if status != web3signer.StatusDeleted {
			r.logger.Warn("Unexpected keystore status",
				zap.String("status", string(status)),
				zap.Stringer("share_pubkey", req.PublicKeys[i]),
			)
		}
	}

	resp := RemoveValidatorResponse{
		Statuses: statuses,
	}

	logger.Debug("Removed validators", fields.Count(len(req.PublicKeys)))
	r.writeJSON(ctx, resp)
}

func (r *Server) handleSignValidator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleSignValidator"))

	body := ctx.PostBody()
	if len(body) == 0 {
		logger.Warn("Request body is empty")
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeString(ctx, "request body is empty")
		return
	}

	var req web3signer.SignRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Warn("Failed to unmarshal request body", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("invalid request body: %w", err))
		return
	}

	identifierValue := ctx.UserValue("identifier")
	sharePubKeyHex, ok := identifierValue.(string)
	if !ok {
		logger.Warn("Unexpected share public key type",
			zap.String("type", fmt.Sprintf("%T", identifierValue)))
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("unexpected share public key type %T", identifierValue))
		return
	}

	sharePubKey, err := hex.DecodeString(strings.TrimPrefix(sharePubKeyHex, "0x"))
	if err != nil {
		logger.Warn("Failed to decode share public key hex", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("decode share public key hex: %w", err))
		return
	}

	if len(sharePubKey) != len(phase0.BLSPubKey{}) {
		logger.Warn("Invalid share public key length",
			zap.Int("length", len(sharePubKey)),
			zap.Int("expected", len(phase0.BLSPubKey{})))
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		r.writeErr(ctx, fmt.Errorf("invalid share public key length %d, expected %d", len(sharePubKey), len(phase0.BLSPubKey{})))
		return
	}

	var blsPubKey phase0.BLSPubKey
	copy(blsPubKey[:], sharePubKey)

	sig, err := r.remoteSigner.Sign(ctx, blsPubKey, req)
	if err != nil {
		logger.Warn("Failed to sign with web3signer", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("failed to sign with Web3Signer: %w", err))
		return
	}

	logger.Debug("Signed payload with validator key", zap.String("type", string(req.Type)))
	r.writeJSON(ctx, sig)
}

func (r *Server) handleOperatorIdentity(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleOperatorIdentity"))

	pubKeyB64, err := r.operatorPrivKey.Public().Base64()
	if err != nil {
		logger.Warn("Failed to get operator public key base64", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("get operator public key base64: %w", err))
		return
	}

	logger.Debug("Got operator public key base64", zap.String("base64", pubKeyB64))
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeString(ctx, pubKeyB64)
}

func (r *Server) handleSignOperator(ctx *fasthttp.RequestCtx) {
	logger := r.logger.With(zap.String("method", "handleSignOperator"))

	payload := ctx.PostBody()

	signature, err := r.operatorPrivKey.Sign(payload)
	if err != nil {
		logger.Warn("Failed to sign message with operator key", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, fmt.Errorf("sign message with operator key: %w", err))
		return
	}

	logger.Debug("Signed message with operator key", zap.Int("payload_size", len(payload)))
	ctx.SetStatusCode(fasthttp.StatusOK)
	r.writeBytes(ctx, signature)
}

func (r *Server) writeString(ctx *fasthttp.RequestCtx, s string) {
	if _, err := ctx.WriteString(s); err != nil {
		r.logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (r *Server) writeBytes(ctx *fasthttp.RequestCtx, b []byte) {
	if _, err := ctx.Write(b); err != nil {
		r.logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

func (r *Server) writeJSON(ctx *fasthttp.RequestCtx, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		r.logger.Error("failed to marshal JSON", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		r.writeErr(ctx, err)
		return
	}

	if _, err := ctx.Write(b); err != nil {
		r.logger.Error("failed to write response", zap.Error(err))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
}

func (r *Server) writeErr(ctx *fasthttp.RequestCtx, err error) {
	if _, writeErr := ctx.WriteString(err.Error()); writeErr != nil {
		r.logger.Error("failed to write response", zap.Error(writeErr))
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}
